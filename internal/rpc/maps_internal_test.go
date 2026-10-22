package rpc

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestMapEntries(t *testing.T) {
	desc := completionMessage(t)
	for _, test := range []struct {
		text, want string
	}{
		// A map written as an array of entries, as Postman takes it.
		{`{"byId": [{"key": "a", "value": {"flag": true}}, {"key": "b", "value": {}}]}`, `{"byId":{"a":{"flag":true},"b":{}}}`},
		{`{"by_id": []}`, `{"by_id":{}}`},
		// Anything else is left as it is.
		{`{"byId": {"a": {}}}`, `{"byId": {"a": {}}}`},
		{`{"byId": [{"key": "a"}]}`, `{"byId": [{"key": "a"}]}`},
		{`{"byId": [{"key": "a", "value": {}, "other": 1}]}`, `{"byId": [{"key": "a", "value": {}, "other": 1}]}`},
		{`{"items": [{"key": "a", "value": {}}]}`, `{"items": [{"key": "a", "value": {}}]}`},
		{`{"byId": [{"key": "a", "value": {}}]} {}`, `{"byId": [{"key": "a", "value": {}}]} {}`},
		{`{oops`, `{oops`},
	} {
		got, err := mapEntries(test.text, desc)
		if err != nil || got != test.want {
			t.Errorf("%s: %s, %v, want %s", test.text, got, err, test.want)
		}
	}

	if _, err := mapEntries(`{"byId": [{"key": "a", "value": {}}, {"key": "a", "value": {}}]}`, desc); err == nil || !strings.Contains(err.Error(), `"a" more than once`) {
		t.Errorf("a key given twice: %v", err)
	}

	// What it gives is read as the map.
	text, err := mapEntries(`{"userName": "x", "byId": [{"key": "a", "value": {"flag": true}}]}`, desc)
	if err != nil {
		t.Fatal(err)
	}
	message := dynamicpb.NewMessage(desc)
	if err := protojson.Unmarshal([]byte(text), message); err != nil {
		t.Fatal(err)
	}
	if got := message.Get(desc.Fields().ByName("by_id")).Map().Len(); got != 1 {
		t.Errorf("%d entries, want 1", got)
	}
}
