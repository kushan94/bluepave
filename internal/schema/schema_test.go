package schema

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestErrorsAreReadableAndComplete(t *testing.T) {
	fsys := fstest.MapFS{"t.schema.json": {Data: []byte(`{
	  "type": "object", "additionalProperties": false, "required": ["name"],
	  "properties": {"name": {"type": "string", "pattern": "^[a-z]+$"}, "size": {"enum": [1, 2]}}}`)}}
	v, err := Load(fsys, "t.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ValidateYAML([]byte("name: ok\nsize: 1\n")); err != nil {
		t.Errorf("valid document: %v", err)
	}
	err = v.ValidateYAML([]byte("name: NOT-OK\nsize: 3\ncolour: blue\n"))
	if err == nil {
		t.Fatal("invalid document: want an error")
	}
	msg := err.Error()
	for _, want := range []string{"/name:", "does not match pattern", "/size:", "colour"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error doesn't mention %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "&{") {
		t.Errorf("error contains a raw Go value:\n%s", msg)
	}
}
