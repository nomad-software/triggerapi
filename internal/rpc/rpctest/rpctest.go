// Package rpctest provides a gRPC server for tests. It serves an Echo
// service which is described through reflection, as defined by this protobuf:
//
//	package test.v1;
//
//	message Inner { bool flag = 1; }
//
//	message EchoRequest {
//	  string text = 1;
//	  int32 count = 2;
//	  Inner inner = 3;
//	  repeated string tags = 4;
//	}
//
//	message EchoResponse {
//	  string text = 1;
//	  string authorization = 2;
//	  string trace = 3;
//	  int32 index = 4;
//	}
//
//	service Echo {
//	  // Say returns the text, along with the authorization and x-trace
//	  // metadata of the call.
//	  rpc Say(EchoRequest) returns (EchoResponse);
//	  // Repeat returns the text count times.
//	  rpc Repeat(EchoRequest) returns (stream EchoResponse);
//	  // Forever returns the text once, then waits until it is cancelled.
//	  rpc Forever(EchoRequest) returns (stream EchoResponse);
//	  // Fail always fails with a not found status.
//	  rpc Fail(EchoRequest) returns (EchoResponse);
//	  // Gather returns the texts of the messages it is sent, joined by
//	  // spaces, with how many there were as its index.
//	  rpc Gather(stream EchoRequest) returns (EchoResponse);
//	  // Talk answers each message it is sent with its text, as it comes.
//	  rpc Talk(stream EchoRequest) returns (stream EchoResponse);
//	}
//
// Only a server made by NewStreamingServer has Gather and Talk, the methods
// which take a stream of messages.
package rpctest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Service is the name of the service the server has.
const Service = "test.v1.Echo"

// Reflection selects which versions of the reflection service a server has.
type Reflection int

const (
	// ReflectionAll serves both versions, as most servers do.
	ReflectionAll Reflection = iota
	// ReflectionAlphaOnly serves only the older version.
	ReflectionAlphaOnly
	// ReflectionNone serves neither.
	ReflectionNone
)

// NewServer starts a server, which the caller must close. Its URL can be used
// as the URL of a request.
func NewServer(reflection Reflection) *httptest.Server {
	return newServer(reflection, false)
}

// NewStreamingServer starts a server as NewServer does, which also has the
// methods that take a stream of messages.
func NewStreamingServer(reflection Reflection) *httptest.Server {
	return newServer(reflection, true)
}

func newServer(reflection Reflection, streaming bool) *httptest.Server {
	files := descriptors(streaming)
	request := message(files, "test.v1.EchoRequest")
	response := message(files, "test.v1.EchoResponse")

	// Request messages have no type until they are given one.
	typed := connect.WithRequestInitializer(func(_ connect.Spec, msg any) error {
		if dynamic, ok := msg.(*dynamicpb.Message); ok {
			*dynamic = *dynamicpb.NewMessage(request)
		}
		return nil
	})

	text := func(m *dynamicpb.Message) string {
		return m.Get(request.Fields().ByName("text")).String()
	}
	reply := func(text string, index int, headers http.Header) *dynamicpb.Message {
		m := dynamicpb.NewMessage(response)
		m.Set(response.Fields().ByName("text"), protoreflect.ValueOfString(text))
		m.Set(response.Fields().ByName("index"), protoreflect.ValueOfInt32(int32(index)))
		m.Set(response.Fields().ByName("authorization"), protoreflect.ValueOfString(headers.Get("Authorization")))
		m.Set(response.Fields().ByName("trace"), protoreflect.ValueOfString(headers.Get("X-Trace")))
		return m
	}

	mux := http.NewServeMux()

	mux.Handle(unary("Say", typed, func(_ context.Context, req *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
		res := connect.NewResponse(reply(text(req.Msg), 0, req.Header()))
		res.Header().Set("X-Server", "rpctest")
		res.Trailer().Set("X-Done", "yes")
		return res, nil
	}))

	mux.Handle(unary("Fail", typed, func(context.Context, *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such thing"))
	}))

	mux.Handle(stream("Repeat", typed, func(_ context.Context, req *connect.Request[dynamicpb.Message], out *connect.ServerStream[dynamicpb.Message]) error {
		count := int(req.Msg.Get(request.Fields().ByName("count")).Int())
		for i := range count {
			if err := out.Send(reply(text(req.Msg), i, req.Header())); err != nil {
				return err
			}
		}
		return nil
	}))

	mux.Handle(stream("Forever", typed, func(ctx context.Context, req *connect.Request[dynamicpb.Message], out *connect.ServerStream[dynamicpb.Message]) error {
		if err := out.Send(reply(text(req.Msg), 0, req.Header())); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}))

	if streaming {
		addStreaming(mux, typed, text, reply)
	}

	reflector := grpcreflect.NewReflector(
		grpcreflect.NamerFunc(func() []string { return []string{Service} }),
		grpcreflect.WithDescriptorResolver(files),
	)
	if reflection == ReflectionAll {
		mux.Handle(grpcreflect.NewHandlerV1(reflector))
	}
	if reflection != ReflectionNone {
		mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
	}

	// gRPC needs HTTP/2, which is served here without TLS.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := httptest.NewUnstartedServer(mux)
	server.Config.Protocols = protocols
	server.Start()

	return server
}

// addStreaming adds the methods which take a stream of messages, which read
// the text of a message with text and answer with reply.
func addStreaming(mux *http.ServeMux, typed connect.HandlerOption, text func(*dynamicpb.Message) string, reply func(string, int, http.Header) *dynamicpb.Message) {
	mux.Handle(clientStream("Gather", typed, func(_ context.Context, in *connect.ClientStream[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
		var texts []string
		for in.Receive() {
			texts = append(texts, text(in.Msg()))
		}
		if err := in.Err(); err != nil {
			return nil, err
		}
		return connect.NewResponse(reply(strings.Join(texts, " "), len(texts), in.RequestHeader())), nil
	}))

	mux.Handle(bidiStream("Talk", typed, func(_ context.Context, chat *connect.BidiStream[dynamicpb.Message, dynamicpb.Message]) error {
		for i := 0; ; i++ {
			in, err := chat.Receive()
			if errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return err
			}
			if err := chat.Send(reply(text(in), i, chat.RequestHeader())); err != nil {
				return err
			}
		}
	}))
}

type unaryFunc = func(context.Context, *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error)
type streamFunc = func(context.Context, *connect.Request[dynamicpb.Message], *connect.ServerStream[dynamicpb.Message]) error
type clientStreamFunc = func(context.Context, *connect.ClientStream[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error)
type bidiStreamFunc = func(context.Context, *connect.BidiStream[dynamicpb.Message, dynamicpb.Message]) error

func unary(name string, option connect.HandlerOption, f unaryFunc) (string, http.Handler) {
	path := "/" + Service + "/" + name
	return path, connect.NewUnaryHandler(path, f, option)
}

func stream(name string, option connect.HandlerOption, f streamFunc) (string, http.Handler) {
	path := "/" + Service + "/" + name
	return path, connect.NewServerStreamHandler(path, f, option)
}

func clientStream(name string, option connect.HandlerOption, f clientStreamFunc) (string, http.Handler) {
	path := "/" + Service + "/" + name
	return path, connect.NewClientStreamHandler(path, f, option)
}

func bidiStream(name string, option connect.HandlerOption, f bidiStreamFunc) (string, http.Handler) {
	path := "/" + Service + "/" + name
	return path, connect.NewBidiStreamHandler(path, f, option)
}

func message(files *protoregistry.Files, name protoreflect.FullName) protoreflect.MessageDescriptor {
	desc, err := files.FindDescriptorByName(name)
	if err != nil {
		panic(err)
	}
	return desc.(protoreflect.MessageDescriptor)
}

// descriptors builds the protobuf definition in the package documentation,
// with the methods which take a stream of messages if streaming is set.
func descriptors(streaming bool) *protoregistry.Files {
	field := func(name string, number int32, kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(name),
			Number: proto.Int32(number),
			Type:   kind.Enum(),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		}
	}
	method := func(name string, clientStreaming, serverStreaming bool) *descriptorpb.MethodDescriptorProto {
		return &descriptorpb.MethodDescriptorProto{
			Name:            proto.String(name),
			InputType:       proto.String(".test.v1.EchoRequest"),
			OutputType:      proto.String(".test.v1.EchoResponse"),
			ClientStreaming: proto.Bool(clientStreaming),
			ServerStreaming: proto.Bool(serverStreaming),
		}
	}

	inner := field("inner", 3, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE)
	inner.TypeName = proto.String(".test.v1.Inner")
	tags := field("tags", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING)
	tags.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test/v1/echo.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:  proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{field("flag", 1, descriptorpb.FieldDescriptorProto_TYPE_BOOL)},
			},
			{
				Name: proto.String("EchoRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("text", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					field("count", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32),
					inner,
					tags,
				},
			},
			{
				Name: proto.String("EchoResponse"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("text", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					field("authorization", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					field("trace", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					field("index", 4, descriptorpb.FieldDescriptorProto_TYPE_INT32),
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("Say", false, false),
				method("Repeat", false, true),
				method("Forever", false, true),
				method("Fail", false, false),
			},
		}},
	}

	if streaming {
		service := file.Service[0]
		service.Method = append(service.Method, method("Gather", true, false), method("Talk", true, true))
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
