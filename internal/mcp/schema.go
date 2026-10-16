package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Item is a tool, a resource or a prompt that a server has.
type Item struct {
	Kind Kind
	// Name is that of a tool or a prompt, or the URI of a resource, which
	// is what a request calls it by.
	Name        string
	Description string
	// input is the schema of the arguments of a tool, and prompt the
	// arguments of a prompt.
	input  any
	prompt []*sdk.PromptArgument
}

// Schema is what a server says it has: its tools, then its resources, then
// its prompts, each in alphabetical order.
type Schema struct {
	Items []Item
	// Server is the name and version the server gave, and Protocol the
	// version of the protocol the client agreed on.
	Server   string
	Protocol string
}

// Discover asks the server for its tools, resources and prompts, as many as
// it says it has: which kinds it has is what it answered to server/discover,
// or to initialize if it is older, which the SDK keeps as the
// InitializeResult.
func (c *Client) Discover(ctx context.Context) (*Schema, error) {
	schema := &Schema{}
	err := c.with(ctx, func(conn *sdk.ClientSession) error {
		schema.Items = nil
		info := conn.InitializeResult()
		if info == nil || info.Capabilities == nil {
			return nil
		}
		schema.Protocol, schema.Server = describe(info)

		var found []Item
		if info.Capabilities.Tools != nil {
			for tool, err := range conn.Tools(ctx, nil) {
				if err != nil {
					return fmt.Errorf("the tools could not be listed: %w", err)
				}
				found = append(found, Item{Kind: KindTool, Name: tool.Name, Description: tool.Description, input: tool.InputSchema})
			}
		}
		if info.Capabilities.Resources != nil {
			for resource, err := range conn.Resources(ctx, nil) {
				if err != nil {
					return fmt.Errorf("the resources could not be listed: %w", err)
				}
				found = append(found, Item{Kind: KindResource, Name: resource.URI, Description: resource.Description})
			}
		}
		if info.Capabilities.Prompts != nil {
			for prompt, err := range conn.Prompts(ctx, nil) {
				if err != nil {
					return fmt.Errorf("the prompts could not be listed: %w", err)
				}
				found = append(found, Item{Kind: KindPrompt, Name: prompt.Name, Description: prompt.Description, prompt: prompt.Arguments})
			}
		}

		order := []Kind{KindTool, KindResource, KindPrompt}
		slices.SortStableFunc(found, func(a, b Item) int {
			if a.Kind != b.Kind {
				return slices.Index(order, a.Kind) - slices.Index(order, b.Kind)
			}
			return strings.Compare(a.Name, b.Name)
		})
		schema.Items = found
		return nil
	})
	if err != nil {
		return nil, err
	}
	return schema, nil
}

// Find returns the item of a kind with a name. A request with no kind is for
// a tool.
func (s *Schema) Find(kind Kind, name string) (Item, bool) {
	if kind == "" {
		kind = KindTool
	}
	index := slices.IndexFunc(s.Items, func(i Item) bool { return i.Kind == kind && i.Name == name })
	if index < 0 {
		return Item{}, false
	}
	return s.Items[index], true
}

// Example returns example arguments for an item, as JSON set out over lines:
// a value for each property of the input of a tool, and an empty string for
// each argument of a prompt. A resource has none, which is empty.
func (i Item) Example() (string, error) {
	var example any
	switch i.Kind {
	case KindResource:
		return "", nil
	case KindPrompt:
		arguments := map[string]any{}
		for _, a := range i.prompt {
			arguments[a.Name] = ""
		}
		example = arguments
	default:
		var input jsonschema.Schema
		if i.input != nil {
			data, err := json.Marshal(i.input)
			if err != nil {
				return "", err
			}
			if err := json.Unmarshal(data, &input); err != nil {
				return "", fmt.Errorf("the schema of the arguments could not be read: %w", err)
			}
		}
		example = exampleOf(&input, &input, 0)
		if _, ok := example.(map[string]any); !ok {
			example = map[string]any{}
		}
	}
	data, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// maxDepth limits how deep an example goes, as a schema can refer to itself.
const maxDepth = 8

// exampleOf returns an example of a value a schema allows: the default or
// example it gives, or else a value of its type, an object having a value
// for each of its properties. root is the schema references are looked up
// in.
func exampleOf(s, root *jsonschema.Schema, depth int) any {
	if s == nil || depth > maxDepth {
		return nil
	}
	if ref, ok := strings.CutPrefix(s.Ref, "#/$defs/"); ok && root.Defs[ref] != nil {
		return exampleOf(root.Defs[ref], root, depth+1)
	}
	if ref, ok := strings.CutPrefix(s.Ref, "#/definitions/"); ok && root.Definitions[ref] != nil {
		return exampleOf(root.Definitions[ref], root, depth+1)
	}

	var given any
	switch {
	case s.Const != nil:
		return *s.Const
	case len(s.Default) > 0 && json.Unmarshal(s.Default, &given) == nil:
		return given
	case len(s.Examples) > 0:
		return s.Examples[0]
	case len(s.Enum) > 0:
		return s.Enum[0]
	}
	for _, choices := range [][]*jsonschema.Schema{s.AnyOf, s.OneOf, s.AllOf} {
		if len(choices) > 0 {
			return exampleOf(choices[0], root, depth+1)
		}
	}

	typ := s.Type
	if typ == "" {
		for _, t := range s.Types {
			if t != "null" {
				typ = t
				break
			}
		}
	}
	if typ == "" && s.Properties != nil {
		typ = "object"
	}

	switch typ {
	case "object":
		object := map[string]any{}
		for name, property := range s.Properties {
			object[name] = exampleOf(property, root, depth+1)
		}
		return object
	case "array":
		if item := exampleOf(s.Items, root, depth+1); item != nil {
			return []any{item}
		}
		return []any{}
	case "string":
		return ""
	case "integer", "number":
		return 0
	case "boolean":
		return false
	}
	return nil
}
