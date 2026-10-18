// Command grpcserver is a small gRPC server to try the gRPC requests of
// TriggerAPI against by hand, with a method of every kind: unary, server
// streaming, client streaming and bidirectional. It describes itself through
// server reflection, serves unencrypted HTTP/2 and logs every call.
//
//	go run ./test/grpcserver                  # localhost:50051
//	go run ./test/grpcserver -addr :9090
//
// Its service is sample.v1.Demo, as defined by this protobuf:
//
//	package sample.v1;
//
//	enum Mood { MOOD_UNSPECIFIED = 0; HAPPY = 1; GRUMPY = 2; CURIOUS = 3; }
//
//	message SayRequest { string text = 1; int32 times = 2; }
//	message SayReply { string text = 1; map<string, string> metadata = 2; }
//	message CountdownRequest { int32 from = 1; int32 delay_ms = 2; }
//	message Tick { int32 remaining = 1; string at = 2; }
//	message Number { double value = 1; string label = 2; }
//	message Total { int32 count = 1; double sum = 2; double mean = 3; repeated string labels = 4; }
//	message ChatMessage { string user = 1; string text = 2; Mood mood = 3; }
//
//	service Demo {
//	  // Say returns the text the number of times asked, and the metadata
//	  // of the call.
//	  rpc Say(SayRequest) returns (SayReply);
//	  // Countdown counts down from a number, a message each step.
//	  rpc Countdown(CountdownRequest) returns (stream Tick);
//	  // Sum adds up the numbers it is sent.
//	  rpc Sum(stream Number) returns (Total);
//	  // Chat answers each message it is sent, as it comes.
//	  rpc Chat(stream ChatMessage) returns (stream ChatMessage);
//	  // Fail always fails, with an invalid argument status.
//	  rpc Fail(SayRequest) returns (SayReply);
//	}
//
// It is not part of the program, and is not used by the tests, which have
// their own server in internal/rpc/rpctest.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// service is the full name of the service.
const service = "sample.v1.Demo"

func main() {
	addr := flag.String("addr", "localhost:50051", "the address to listen on")
	flag.Parse()

	files := descriptors()
	d := &demo{files: files}
	mux := http.NewServeMux()
	d.register(mux)

	reflector := grpcreflect.NewReflector(
		grpcreflect.NamerFunc(func() []string { return []string{service} }),
		grpcreflect.WithDescriptorResolver(files),
	)
	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

	// gRPC needs HTTP/2, which is served here without TLS.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{Addr: *addr, Handler: mux, Protocols: protocols}

	log.Printf("gRPC server with the service %s at %s, without TLS", service, *addr)
	log.Fatal(server.ListenAndServe())
}

// demo is the service, which makes and reads its messages through their
// descriptors, as there is no generated code.
type demo struct {
	files *protoregistry.Files
}

// message returns a new message of a type of the service.
func (d *demo) message(name string) *dynamicpb.Message {
	desc, err := d.files.FindDescriptorByName(protoreflect.FullName("sample.v1." + name))
	if err != nil {
		panic(err)
	}
	return dynamicpb.NewMessage(desc.(protoreflect.MessageDescriptor))
}

// get returns the value of a field of a message.
func get(m *dynamicpb.Message, name string) protoreflect.Value {
	return m.Get(m.Descriptor().Fields().ByName(protoreflect.Name(name)))
}

// set gives a field of a message a value.
func set(m *dynamicpb.Message, name string, value protoreflect.Value) {
	m.Set(m.Descriptor().Fields().ByName(protoreflect.Name(name)), value)
}

// typed has the requests of a method read as the type of message it takes,
// which they have none of until they are given it.
func (d *demo) typed(input string) connect.HandlerOption {
	return connect.WithRequestInitializer(func(_ connect.Spec, message any) error {
		if dynamic, ok := message.(*dynamicpb.Message); ok {
			*dynamic = *d.message(input)
		}
		return nil
	})
}

// register adds the methods of the service.
func (d *demo) register(mux *http.ServeMux) {
	path := func(method string) string { return "/" + service + "/" + method }

	mux.Handle(path("Say"), connect.NewUnaryHandler(path("Say"), d.say, d.typed("SayRequest")))
	mux.Handle(path("Countdown"), connect.NewServerStreamHandler(path("Countdown"), d.countdown, d.typed("CountdownRequest")))
	mux.Handle(path("Sum"), connect.NewClientStreamHandler(path("Sum"), d.sum, d.typed("Number")))
	mux.Handle(path("Chat"), connect.NewBidiStreamHandler(path("Chat"), d.chat, d.typed("ChatMessage")))
	mux.Handle(path("Fail"), connect.NewUnaryHandler(path("Fail"), d.fail, d.typed("SayRequest")))
}

func (d *demo) say(_ context.Context, req *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
	text := get(req.Msg, "text").String()
	times := max(int(get(req.Msg, "times").Int()), 1)
	log.Printf("Say %q %d times", text, times)

	reply := d.message("SayReply")
	set(reply, "text", protoreflect.ValueOfString(strings.TrimSpace(strings.Repeat(text+" ", times))))
	metadata := reply.Mutable(reply.Descriptor().Fields().ByName("metadata")).Map()
	for name, values := range req.Header() {
		metadata.Set(protoreflect.ValueOfString(strings.ToLower(name)).MapKey(), protoreflect.ValueOfString(strings.Join(values, ", ")))
	}
	return connect.NewResponse(reply), nil
}

func (d *demo) countdown(ctx context.Context, req *connect.Request[dynamicpb.Message], out *connect.ServerStream[dynamicpb.Message]) error {
	from := int(get(req.Msg, "from").Int())
	if from <= 0 {
		from = 5
	}
	delay := time.Duration(get(req.Msg, "delay_ms").Int()) * time.Millisecond
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}
	log.Printf("Countdown from %d, a step every %s", from, delay)

	for remaining := from; remaining >= 0; remaining-- {
		tick := d.message("Tick")
		set(tick, "remaining", protoreflect.ValueOfInt32(int32(remaining)))
		set(tick, "at", protoreflect.ValueOfString(time.Now().Format(time.TimeOnly+".000")))
		if err := out.Send(tick); err != nil {
			return err
		}
		if remaining == 0 {
			break
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			log.Printf("Countdown given up at %d", remaining)
			return ctx.Err()
		}
	}
	return nil
}

func (d *demo) sum(_ context.Context, in *connect.ClientStream[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
	total := d.message("Total")
	labels := total.Mutable(total.Descriptor().Fields().ByName("labels")).List()
	count, sum := 0, 0.0
	for in.Receive() {
		value := get(in.Msg(), "value").Float()
		log.Printf("Sum was sent %v", value)
		count++
		sum += value
		if label := get(in.Msg(), "label").String(); label != "" {
			labels.Append(protoreflect.ValueOfString(label))
		}
	}
	if err := in.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("there were no numbers to add up"))
	}
	log.Printf("Sum of %d numbers is %v", count, sum)

	set(total, "count", protoreflect.ValueOfInt32(int32(count)))
	set(total, "sum", protoreflect.ValueOfFloat64(sum))
	set(total, "mean", protoreflect.ValueOfFloat64(sum/float64(count)))
	return connect.NewResponse(total), nil
}

// moods are what Chat answers each mood with.
var moods = map[string]string{
	"HAPPY":   "Glad to hear it!",
	"GRUMPY":  "Sorry to hear that.",
	"CURIOUS": "Ask away.",
}

func (d *demo) chat(_ context.Context, stream *connect.BidiStream[dynamicpb.Message, dynamicpb.Message]) error {
	for n := 1; ; n++ {
		in, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			log.Printf("Chat ended after %d messages", n-1)
			return nil
		} else if err != nil {
			return err
		}
		user := get(in, "user").String()
		text := get(in, "text").String()
		mood := in.Descriptor().Fields().ByName("mood").Enum().Values().ByNumber(get(in, "mood").Enum())
		log.Printf("Chat message %d from %q: %q", n, user, text)

		answer := fmt.Sprintf("Message %d: you said %q.", n, text)
		if mood != nil {
			if reply, ok := moods[string(mood.Name())]; ok {
				answer += " " + reply
			}
		}
		out := d.message("ChatMessage")
		set(out, "user", protoreflect.ValueOfString("demo"))
		set(out, "text", protoreflect.ValueOfString(answer))
		set(out, "mood", get(in, "mood"))
		if err := stream.Send(out); err != nil {
			return err
		}
	}
}

func (d *demo) fail(context.Context, *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
	log.Print("Fail")
	return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this method always fails"))
}

// descriptors builds the protobuf definition in the package documentation.
func descriptors() *protoregistry.Files {
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	field := func(name string, number int32, kind descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(number), Type: kind.Enum(), Label: optional}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}
		return f
	}
	repeated := func(f *descriptorpb.FieldDescriptorProto) *descriptorpb.FieldDescriptorProto {
		f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
		return f
	}
	message := func(name string, fields ...*descriptorpb.FieldDescriptorProto) *descriptorpb.DescriptorProto {
		return &descriptorpb.DescriptorProto{Name: proto.String(name), Field: fields}
	}
	method := func(name, input, output string, clientStreaming, serverStreaming bool) *descriptorpb.MethodDescriptorProto {
		return &descriptorpb.MethodDescriptorProto{
			Name:            proto.String(name),
			InputType:       proto.String(".sample.v1." + input),
			OutputType:      proto.String(".sample.v1." + output),
			ClientStreaming: proto.Bool(clientStreaming),
			ServerStreaming: proto.Bool(serverStreaming),
		}
	}
	const (
		str     = descriptorpb.FieldDescriptorProto_TYPE_STRING
		integer = descriptorpb.FieldDescriptorProto_TYPE_INT32
		double  = descriptorpb.FieldDescriptorProto_TYPE_DOUBLE
		enum    = descriptorpb.FieldDescriptorProto_TYPE_ENUM
		msg     = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	)

	// A map is a list of entries of a message of its own.
	entry := message("MetadataEntry", field("key", 1, str, ""), field("value", 2, str, ""))
	entry.Options = &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)}
	reply := message("SayReply", field("text", 1, str, ""), repeated(field("metadata", 2, msg, ".sample.v1.SayReply.MetadataEntry")))
	reply.NestedType = []*descriptorpb.DescriptorProto{entry}

	value := func(name string, number int32) *descriptorpb.EnumValueDescriptorProto {
		return &descriptorpb.EnumValueDescriptorProto{Name: proto.String(name), Number: proto.Int32(number)}
	}

	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("sample/v1/demo.proto"),
		Package: proto.String("sample.v1"),
		Syntax:  proto.String("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name:  proto.String("Mood"),
			Value: []*descriptorpb.EnumValueDescriptorProto{value("MOOD_UNSPECIFIED", 0), value("HAPPY", 1), value("GRUMPY", 2), value("CURIOUS", 3)},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			message("SayRequest", field("text", 1, str, ""), field("times", 2, integer, "")),
			reply,
			message("CountdownRequest", field("from", 1, integer, ""), field("delay_ms", 2, integer, "")),
			message("Tick", field("remaining", 1, integer, ""), field("at", 2, str, "")),
			message("Number", field("value", 1, double, ""), field("label", 2, str, "")),
			message("Total", field("count", 1, integer, ""), field("sum", 2, double, ""), field("mean", 3, double, ""), repeated(field("labels", 4, str, ""))),
			message("ChatMessage", field("user", 1, str, ""), field("text", 2, str, ""), field("mood", 3, enum, ".sample.v1.Mood")),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Demo"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("Say", "SayRequest", "SayReply", false, false),
				method("Countdown", "CountdownRequest", "Tick", false, true),
				method("Sum", "Number", "Total", true, false),
				method("Chat", "ChatMessage", "ChatMessage", true, true),
				method("Fail", "SayRequest", "SayReply", false, false),
			},
		}},
	}

	desc, err := protodesc.NewFile(file, nil)
	if err != nil {
		panic(err)
	}
	files := &protoregistry.Files{}
	if err := files.RegisterFile(desc); err != nil {
		panic(err)
	}
	return files
}
