package rpc

import (
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/nomad-software/triggerapi/internal/completion"
)

// completionMessage returns a message with a field of each kind there is
// something to complete in:
//
//	enum Role { ROLE_UNSPECIFIED = 0; ADMIN = 1; }
//	message Inner { bool flag = 1; Role role = 2; }
//	message Req {
//	  string user_name = 1;
//	  Role role = 2;
//	  repeated Inner items = 3;
//	  map<string, Inner> by_id = 4;
//	  repeated Role roles = 5;
//	  bool active = 6;
//	  google.protobuf.Timestamp at = 7;
//	}
func completionMessage(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	field := func(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, typeName string, repeated bool) *descriptorpb.FieldDescriptorProto {
		label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		if repeated {
			label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
		}
		f := &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(number), Type: typ.Enum(), Label: label.Enum()}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}
		return f
	}
	enum := descriptorpb.FieldDescriptorProto_TYPE_ENUM
	message := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	file := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("complete.proto"),
		Package:    proto.String("c"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/timestamp.proto"},
		EnumType: []*descriptorpb.EnumDescriptorProto{{Name: proto.String("Role"), Value: []*descriptorpb.EnumValueDescriptorProto{
			{Name: proto.String("ROLE_UNSPECIFIED"), Number: proto.Int32(0)},
			{Name: proto.String("ADMIN"), Number: proto.Int32(1)},
		}}},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Inner"), Field: []*descriptorpb.FieldDescriptorProto{
				field("flag", 1, descriptorpb.FieldDescriptorProto_TYPE_BOOL, "", false),
				field("role", 2, enum, ".c.Role", false),
			}},
			{Name: proto.String("Req"), Field: []*descriptorpb.FieldDescriptorProto{
				field("user_name", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, "", false),
				field("role", 2, enum, ".c.Role", false),
				field("items", 3, message, ".c.Inner", true),
				field("by_id", 4, message, ".c.Req.ByIdEntry", true),
				field("roles", 5, enum, ".c.Role", true),
				field("active", 6, descriptorpb.FieldDescriptorProto_TYPE_BOOL, "", false),
				field("at", 7, message, ".google.protobuf.Timestamp", false),
			}, NestedType: []*descriptorpb.DescriptorProto{{
				Name:    proto.String("ByIdEntry"),
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
				Field: []*descriptorpb.FieldDescriptorProto{
					field("key", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, "", false),
					field("value", 2, message, ".c.Inner", false),
				},
			}}},
		},
	}
	fd, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Messages().ByName("Req")
}

// complete returns the labels of what can be typed where the message has a
// |, which is taken out.
func complete(desc protoreflect.MessageDescriptor, message string) ([]string, completion.Completion) {
	offset := strings.Index(message, "|")
	message = strings.Replace(message, "|", "", 1)
	c := completeMessage(desc, message, offset)
	var got []string
	for _, item := range c.Items {
		got = append(got, item.Label)
	}
	return got, c
}

func TestCompleteMessage(t *testing.T) {
	desc := completionMessage(t)
	all := []string{"active", "at", "byId", "items", "role", "roles", "userName"}
	for _, test := range []struct {
		message string
		want    []string
	}{
		// The fields of the message, by their JSON names, in quotes or not.
		{"{|", all},
		{`{"|`, all},
		{`{"ro|`, []string{"role", "roles"}},
		{`{ro|`, []string{"role", "roles"}},
		// Not those there are already, by either name.
		{`{"role": 1, "user_name": "x", |`, []string{"active", "at", "byId", "items", "roles"}},
		// The values of an enum and a boolean.
		{`{"role": |`, []string{"ADMIN", "ROLE_UNSPECIFIED"}},
		{`{"role": "A|`, []string{"ADMIN"}},
		{`{"active": |`, []string{"false", "true"}},
		{`{"active": "|`, nil},
		{`{"roles": [|`, []string{"ADMIN", "ROLE_UNSPECIFIED"}},
		{`{"roles": ["ADMIN", |`, []string{"ADMIN", "ROLE_UNSPECIFIED"}},
		{`{"roles": ["ADMIN" |`, nil},
		// Into a list of messages, and the values of a map.
		{`{"items": [{|`, []string{"flag", "role"}},
		{`{"items": [{"flag": true}, {"r|`, []string{"role"}},
		{`{"byId": {"a": {|`, []string{"flag", "role"}},
		{`{"byId": {|`, nil},
		// Out of a nested message again.
		{`{"items": [{"flag": true}], "u|`, []string{"userName"}},
		// Nothing for a value which is a string, inside protobuf's own
		// types, or before the message.
		{`{"userName": |`, nil},
		{`{"at": {|`, nil},
		{`|`, nil},
		// A quote in a string does not end it.
		{`{"userName": "a\"b", "act|`, []string{"active"}},
	} {
		if got, _ := complete(desc, test.message); !slices.Equal(got, test.want) {
			t.Errorf("%s: %q, want %q", test.message, got, test.want)
		}
	}
}

func TestCompleteMessageItems(t *testing.T) {
	desc := completionMessage(t)
	_, c := complete(desc, `{"us|`)
	if c.Start != 1 || c.End != 4 || len(c.Items) != 1 || c.Items[0] != (completion.Item{Label: "userName", Detail: "string", Insert: `"userName": `}) {
		t.Errorf("key: %+v", c)
	}
	// With the closing quote there already, it is not typed again.
	_, c = complete(desc, `{"us|"`)
	if len(c.Items) != 1 || c.Items[0].Insert != `"userName` {
		t.Errorf("closed key: %+v", c)
	}
	_, c = complete(desc, `{by|`)
	if c.Start != 1 || c.Items[0] != (completion.Item{Label: "byId", Detail: "map<string, Inner>", Insert: `"byId": `}) {
		t.Errorf("bare key: %+v", c)
	}
	_, c = complete(desc, `{"roles": [A|`)
	if c.Items[0] != (completion.Item{Label: "ADMIN", Detail: "Role", Insert: `"ADMIN"`}) {
		t.Errorf("enum: %+v", c)
	}
	_, c = complete(desc, `{"items": |`)
	if c.Items != nil {
		t.Errorf("list: %+v", c)
	}
	_, c = complete(desc, `{|`)
	if i := slices.IndexFunc(c.Items, func(item completion.Item) bool { return item.Label == "items" }); i < 0 || c.Items[i].Detail != "repeated Inner" {
		t.Errorf("repeated: %+v", c.Items)
	}
}

func TestCompleteUnknownMethod(t *testing.T) {
	if c := (&Schema{}).Complete("a.B/C", "{", 1); c.Items != nil {
		t.Errorf("%+v", c)
	}
}
