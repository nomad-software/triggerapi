package rpc

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// The resolver a server's files are built with looks in each set of files
// in turn, so a file can import one the server sent earlier or one that is
// built in.
func TestFallbackResolver(t *testing.T) {
	builtIn := descriptorpb.File_google_protobuf_descriptor_proto
	resolver := fallbackResolver{new(protoregistry.Files), protoregistry.GlobalFiles}

	if file, err := resolver.FindFileByPath(builtIn.Path()); err != nil || file != builtIn {
		t.Errorf("file %v, %v", file, err)
	}
	name := builtIn.Messages().ByName("FileDescriptorProto").FullName()
	if desc, err := resolver.FindDescriptorByName(name); err != nil || desc.FullName() != name {
		t.Errorf("descriptor %v, %v", desc, err)
	}

	if _, err := resolver.FindFileByPath("none.proto"); !errors.Is(err, protoregistry.NotFound) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := resolver.FindDescriptorByName("none.Message"); !errors.Is(err, protoregistry.NotFound) {
		t.Errorf("missing descriptor: %v", err)
	}
	if _, err := (fallbackResolver{}).FindFileByPath(builtIn.Path()); !errors.Is(err, protoregistry.NotFound) {
		t.Errorf("found with no files: %v", err)
	}
}
