package rpc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	// These make the well-known types available for servers that do not
	// describe them.
	_ "google.golang.org/protobuf/types/known/anypb"
	_ "google.golang.org/protobuf/types/known/durationpb"
	_ "google.golang.org/protobuf/types/known/emptypb"
	_ "google.golang.org/protobuf/types/known/fieldmaskpb"
	_ "google.golang.org/protobuf/types/known/structpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	_ "google.golang.org/protobuf/types/known/wrapperspb"
)

// Method is a method of a service on the server.
type Method struct {
	// Name is the full name of the method, as package.Service/Method.
	Name            string
	ClientStreaming bool
	ServerStreaming bool

	desc protoreflect.MethodDescriptor
}

// Schema describes the services of a server.
type Schema struct {
	// Methods are sorted by name.
	Methods []Method

	types *dynamicpb.Types
}

// Method finds a method by its full name.
func (s *Schema) Method(name string) (Method, bool) {
	for _, m := range s.Methods {
		if m.Name == name {
			return m, true
		}
	}
	return Method{}, false
}

// Describe returns a summary of the services and their methods in the style
// of a protobuf service definition.
func (s *Schema) Describe() string {
	var b strings.Builder

	service := protoreflect.FullName("")
	for _, m := range s.Methods {
		if parent := m.desc.Parent().FullName(); parent != service {
			if service != "" {
				b.WriteString("}\n\n")
			}
			service = parent
			fmt.Fprintf(&b, "service %s {\n", service)
		}

		in, out := string(m.desc.Input().FullName()), string(m.desc.Output().FullName())
		if m.ClientStreaming {
			in = "stream " + in
		}
		if m.ServerStreaming {
			out = "stream " + out
		}
		fmt.Fprintf(&b, "  rpc %s(%s) returns (%s);\n", m.desc.Name(), in, out)
	}
	if service != "" {
		b.WriteString("}\n")
	}

	return b.String()
}

// Example returns a JSON message for the method with every field present, as
// a starting point to edit.
func (s *Schema) Example(method string) (string, error) {
	m, ok := s.Method(method)
	if !ok {
		return "", fmt.Errorf("the server does not have a method named %s", method)
	}

	message := dynamicpb.NewMessage(m.desc.Input())
	populate(message, map[protoreflect.FullName]bool{})

	data, err := protojson.MarshalOptions{Resolver: s.types, EmitUnpopulated: true}.Marshal(message)
	if err != nil {
		return "", err
	}
	return indent(data)
}

// populate creates the nested messages of message, and gives its lists one
// element, so that their fields show up in the example. Scalar fields are
// left for the encoder to emit with their default values. A message type is
// not created inside itself, which would never end.
func populate(message protoreflect.Message, within map[protoreflect.FullName]bool) {
	name := message.Descriptor().FullName()
	within[name] = true
	defer delete(within, name)

	fields := message.Descriptor().Fields()
	for i := range fields.Len() {
		field := fields.Get(i)

		// Only the first member of a oneof can be set.
		if oneof := field.ContainingOneof(); oneof != nil && oneof.Fields().Get(0) != field {
			continue
		}

		nested := field.Message()
		isMessage := nested != nil && !field.IsMap()
		if isMessage && (within[nested.FullName()] || isWellKnown(nested)) {
			continue
		}

		switch {
		case field.IsMap():
		case field.IsList():
			list := message.Mutable(field).List()
			if isMessage {
				populate(list.AppendMutable().Message(), within)
			} else {
				list.Append(list.NewElement())
			}
		case isMessage:
			populate(message.Mutable(field).Message(), within)
		case field.ContainingOneof() != nil:
			message.Set(field, field.Default())
		}
	}
}

// isWellKnown reports if the message is one of protobuf's own types, which
// have special JSON forms that an empty value does not always suit.
func isWellKnown(message protoreflect.MessageDescriptor) bool {
	return message.ParentFile().Package() == "google.protobuf"
}

// Reflect asks the server of the request to describe its services.
func Reflect(ctx context.Context, r Request) (*Schema, error) {
	target, httpClient, closer, err := r.client()
	if err != nil {
		return nil, err
	}
	defer closer()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The client uses the newer version of the reflection service and falls
	// back to the older one, which is all many servers have.
	stream := grpcreflect.NewClient(httpClient, target, connect.WithGRPC()).
		NewStream(ctx, grpcreflect.WithRequestHeaders(r.headers()))
	defer stream.Close()

	schema, err := reflect(stream)
	if connect.CodeOf(err) == connect.CodeUnimplemented {
		return nil, errors.New("the server does not support reflection, so its services cannot be discovered")
	}
	return schema, err
}

// reflect reads the schema from a reflection stream.
func reflect(stream *grpcreflect.ClientStream) (*Schema, error) {
	names, err := stream.ListServices()
	if err != nil {
		return nil, err
	}

	var services []protoreflect.FullName
	for _, name := range names {
		// The reflection service itself is of no interest.
		if !strings.HasPrefix(string(name), "grpc.reflection.") {
			services = append(services, name)
		}
	}

	// Collect the files that define the services, then the files those
	// import, until everything is known.
	protos := map[string]*descriptorpb.FileDescriptorProto{}
	add := func(files []*descriptorpb.FileDescriptorProto) {
		for _, file := range files {
			protos[file.GetName()] = file
		}
	}

	for _, service := range services {
		files, err := stream.FileContainingSymbol(service)
		if err != nil {
			return nil, err
		}
		add(files)
	}

	asked := map[string]bool{}
	for {
		var missing []string
		for _, file := range protos {
			for _, dep := range file.GetDependency() {
				if protos[dep] == nil && !asked[dep] {
					missing = append(missing, dep)
					asked[dep] = true
				}
			}
		}
		if len(missing) == 0 {
			break
		}

		for _, name := range missing {
			files, err := stream.FileByFilename(name)
			if grpcreflect.IsReflectionStreamBroken(err) {
				return nil, err
			}
			// A file the server cannot provide may be one this program
			// already knows.
			if err == nil {
				add(files)
			}
		}
	}

	files, err := link(protos)
	if err != nil {
		return nil, err
	}

	schema := &Schema{types: dynamicpb.NewTypes(files)}
	for _, name := range services {
		desc, err := files.FindDescriptorByName(name)
		if err != nil {
			return nil, fmt.Errorf("the server did not describe its service %s", name)
		}
		service, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			return nil, fmt.Errorf("%s is not a service", name)
		}

		methods := service.Methods()
		for i := range methods.Len() {
			m := methods.Get(i)
			schema.Methods = append(schema.Methods, Method{
				Name:            string(name) + "/" + string(m.Name()),
				ClientStreaming: m.IsStreamingClient(),
				ServerStreaming: m.IsStreamingServer(),
				desc:            m,
			})
		}
	}
	slices.SortFunc(schema.Methods, func(a, b Method) int { return strings.Compare(a.Name, b.Name) })

	return schema, nil
}

// link turns the file descriptor messages into descriptors that refer to each
// other, building the files a file imports before the file itself.
func link(protos map[string]*descriptorpb.FileDescriptorProto) (*protoregistry.Files, error) {
	files := &protoregistry.Files{}
	building := map[string]bool{}

	// Imports that the server did not provide are looked for among the files
	// built into this program.
	resolver := fallbackResolver{files, protoregistry.GlobalFiles}
	options := protodesc.FileOptions{AllowUnresolvable: true}

	var build func(name string) error
	build = func(name string) error {
		if _, err := files.FindFileByPath(name); err == nil {
			return nil
		}
		file := protos[name]
		if file == nil {
			return nil
		}
		if building[name] {
			return fmt.Errorf("%s imports itself", name)
		}
		building[name] = true

		for _, dep := range file.GetDependency() {
			if err := build(dep); err != nil {
				return err
			}
		}

		desc, err := options.New(file, resolver)
		if err != nil {
			return err
		}
		return files.RegisterFile(desc)
	}

	for name := range protos {
		if err := build(name); err != nil {
			return nil, err
		}
	}

	return files, nil
}

// fallbackResolver looks in each set of files in turn.
type fallbackResolver []*protoregistry.Files

func (r fallbackResolver) FindFileByPath(path string) (protoreflect.FileDescriptor, error) {
	for _, files := range r {
		if desc, err := files.FindFileByPath(path); err == nil {
			return desc, nil
		}
	}
	return nil, protoregistry.NotFound
}

func (r fallbackResolver) FindDescriptorByName(name protoreflect.FullName) (protoreflect.Descriptor, error) {
	for _, files := range r {
		if desc, err := files.FindDescriptorByName(name); err == nil {
			return desc, nil
		}
	}
	return nil, protoregistry.NotFound
}
