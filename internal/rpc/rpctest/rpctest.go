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
//	}
package rpctest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

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
	files := descriptors()
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

type unaryFunc = func(context.Context, *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error)
type streamFunc = func(context.Context, *connect.Request[dynamicpb.Message], *connect.ServerStream[dynamicpb.Message]) error

func unary(name string, option connect.HandlerOption, f unaryFunc) (string, http.Handler) {
	path := "/" + Service + "/" + name
	return path, connect.NewUnaryHandler(path, f, option)
}

func stream(name string, option connect.HandlerOption, f streamFunc) (string, http.Handler) {
	path := "/" + Service + "/" + name
	return path, connect.NewServerStreamHandler(path, f, option)
}

func message(files *protoregistry.Files, name protoreflect.FullName) protoreflect.MessageDescriptor {
	desc, err := files.FindDescriptorByName(name)
	if err != nil {
		panic(err)
	}
	return desc.(protoreflect.MessageDescriptor)
}

// descriptors builds the protobuf definition in the package documentation.
func descriptors() *protoregistry.Files {
	field := func(name string, number int32, kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(name),
			Number: proto.Int32(number),
			Type:   kind.Enum(),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		}
	}
	method := func(name string, serverStreaming bool) *descriptorpb.MethodDescriptorProto {
		return &descriptorpb.MethodDescriptorProto{
			Name:            proto.String(name),
			InputType:       proto.String(".test.v1.EchoRequest"),
			OutputType:      proto.String(".test.v1.EchoResponse"),
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
				method("Say", false),
				method("Repeat", true),
				method("Forever", true),
				method("Fail", false),
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
