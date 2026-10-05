// Package discovered reads and writes .bluepave/discovered.yaml: identifiers `bluepave up` finds or
// creates (never secrets), which Bicep and the GitOps root chart read (ADR-0002).
//
// Layout, under the top-level key "discovered":
//
//	azure:        { tenantId, subscriptionId }
//	admins:       { groupObjectId }
//	ci:           { principalId, clientId }
//	global:       { <module>: { <output>: value } }        infra outputs of global modules
//	environments: { <env>: { <module>: { <name>: value } } } infra outputs and IDs per environment
package discovered

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Path is the file's location, relative to the repository root.
const Path = ".bluepave/discovered.yaml"

const header = `# IDs that ` + "`bluepave up`" + ` discovers or creates in Azure and GitHub, and that GitOps and Bicep read.
# Written by the CLI and committed: they're identifiers, not secrets (secrets stay in Key Vault).
# Empty until ` + "`bluepave up`" + ` has run; modules skip what depends on a missing ID. Layout (ADR-0002):
#   discovered.azure.{tenantId,subscriptionId}, discovered.admins.groupObjectId,
#   discovered.ci.{principalId,clientId}, discovered.global.<module>.<output>,
#   discovered.environments.<env>.<module>.<output or ID>
`

// IDs is the content under "discovered".
type IDs map[string]any

// Load reads the file under root. A missing file is an empty set of IDs.
func Load(root string) (IDs, error) {
	data, err := os.ReadFile(filepath.Join(root, Path))
	if os.IsNotExist(err) {
		return IDs{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Discovered IDs `yaml:"discovered"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", Path, err)
	}
	if doc.Discovered == nil {
		doc.Discovered = IDs{}
	}
	return doc.Discovered, nil
}

// Save writes the file under root, keys sorted, with the explanatory header.
func (ids IDs) Save(root string) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(map[string]any{"discovered": map[string]any(ids)}); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	path := filepath.Join(root, Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Get returns the value at a path of keys, or nil.
func (ids IDs) Get(keys ...string) any {
	var cur any = map[string]any(ids)
	for _, k := range keys {
		m, ok := asMap(cur)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// Set stores a value at a path of keys, creating maps as needed.
func (ids IDs) Set(value any, keys ...string) {
	cur := map[string]any(ids)
	for _, k := range keys[:len(keys)-1] {
		next, ok := asMap(cur[k])
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	cur[keys[len(keys)-1]] = value
}

// Outputs returns a module's recorded outputs: global when env is "", else per environment.
func (ids IDs) Outputs(env, module string) map[string]any {
	var v any
	if env == "" {
		v = ids.Get("global", module)
	} else {
		v = ids.Get("environments", env, module)
	}
	m, _ := asMap(v)
	return m
}

// SetOutputs records a module's outputs (merged into what's there: IDs the CLI created for the
// module, like an Entra app's client ID, stay).
func (ids IDs) SetOutputs(env, module string, outputs map[string]any) {
	for k, v := range outputs {
		if env == "" {
			ids.Set(v, "global", module, k)
		} else {
			ids.Set(v, "environments", env, module, k)
		}
	}
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case IDs:
		return map[string]any(m), true
	}
	return nil, false
}
