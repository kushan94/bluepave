// Package schema validates YAML documents against the bluepave JSON Schemas.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var english = message.NewPrinter(language.English)

// Validator validates documents against one schema.
type Validator struct {
	schema *jsonschema.Schema
}

// Load compiles the named schema (e.g. "platform.schema.json") from fsys.
func Load(fsys fs.FS, name string) (*Validator, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read schema %s: %w", name, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", name, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		return nil, fmt.Errorf("add schema %s: %w", name, err)
	}
	s, err := c.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", name, err)
	}
	return &Validator{schema: s}, nil
}

// ValidateYAML parses YAML and validates it. The returned error lists every violation, each as
// "<location>: <problem>", so a user can fix them all at once.
func (v *Validator) ValidateYAML(data []byte) error {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("not valid YAML: %w", err)
	}
	// Round-trip through JSON: the validator works on JSON types (YAML maps become map[string]any).
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("convert to JSON: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := v.schema.Validate(inst); err != nil {
		var verr *jsonschema.ValidationError
		if ok := asValidationError(err, &verr); ok {
			return fmt.Errorf("%s", strings.Join(flatten(verr), "\n"))
		}
		return err
	}
	return nil
}

func asValidationError(err error, target **jsonschema.ValidationError) bool {
	v, ok := err.(*jsonschema.ValidationError)
	if ok {
		*target = v
	}
	return ok
}

// flatten turns the validator's error tree into one line per leaf problem.
func flatten(e *jsonschema.ValidationError) []string {
	if len(e.Causes) == 0 {
		loc := "/" + strings.Join(e.InstanceLocation, "/")
		return []string{fmt.Sprintf("%s: %s", loc, e.ErrorKind.LocalizedString(english))}
	}
	var out []string
	for _, c := range e.Causes {
		out = append(out, flatten(c)...)
	}
	return out
}
