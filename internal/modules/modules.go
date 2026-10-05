// Package modules discovers module manifests and checks that a set of enabled modules is complete
// and consistent (ADR-0001).
package modules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"go.yaml.in/yaml/v3"

	v1alpha1 "github.com/kushan94/bluepave/api/v1alpha1"
	"github.com/kushan94/bluepave/internal/schema"
)

// Module is a parsed modules/<name>/module.yaml.
type Module struct {
	Dir      string `yaml:"-"`
	Metadata struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	} `yaml:"metadata"`
	Spec struct {
		Version  string            `yaml:"version"`
		Requires []string          `yaml:"requires"`
		Outputs  []string          `yaml:"outputs"`
		Layers   map[string]string `yaml:"layers"`
		Tests    string            `yaml:"tests"`
		Config   *struct {
			Schema string `yaml:"schema"`
		} `yaml:"config"`
	} `yaml:"spec"`
}

// Discover loads every modules/*/module.yaml under root, validating each against the module
// schema and checking that the files it points to exist. It returns all problems, not just the
// first.
func Discover(root string) (map[string]*Module, error) {
	v, err := schema.Load(v1alpha1.Schemas, "module.schema.json")
	if err != nil {
		return nil, err
	}
	manifests, err := filepath.Glob(filepath.Join(root, "modules", "*", "module.yaml"))
	if err != nil {
		return nil, err
	}
	found := map[string]*Module{}
	var problems []error
	for _, path := range manifests {
		m, err := load(v, path)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if dir := filepath.Base(m.Dir); dir != m.Metadata.Name {
			problems = append(problems, fmt.Errorf("%s: metadata.name %q must match its folder %q", path, m.Metadata.Name, dir))
			continue
		}
		found[m.Metadata.Name] = m
	}
	return found, errors.Join(problems...)
}

func load(v *schema.Validator, path string) (*Module, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := v.ValidateYAML(data); err != nil {
		return nil, fmt.Errorf("%s is not a valid module manifest:\n%w", path, err)
	}
	var m Module
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	m.Dir = filepath.Dir(path)
	var problems []error
	paths := map[string]string{"spec.tests": m.Spec.Tests}
	for layer, p := range m.Spec.Layers {
		paths["spec.layers."+layer] = p
	}
	if m.Spec.Config != nil {
		paths["spec.config.schema"] = m.Spec.Config.Schema
	}
	for field, p := range paths {
		if _, err := os.Stat(filepath.Join(m.Dir, p)); err != nil {
			problems = append(problems, fmt.Errorf("%s: %s points to %q, which doesn't exist", path, field, p))
		}
	}
	return &m, errors.Join(problems...)
}

// Resolve checks that every enabled module exists and that its requirements are enabled too.
// It returns the modules in dependency order (a module after everything it requires).
func Resolve(available map[string]*Module, enabled []string) ([]*Module, error) {
	on := map[string]bool{}
	for _, n := range enabled {
		on[n] = true
	}
	var problems []error
	for _, n := range enabled {
		m, ok := available[n]
		if !ok {
			problems = append(problems, fmt.Errorf("module %q is enabled but doesn't exist (available: %v)", n, names(available)))
			continue
		}
		for _, r := range m.Spec.Requires {
			if !on[r] {
				problems = append(problems, fmt.Errorf("module %q requires %q, which isn't enabled", n, r))
			}
		}
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return order(available, enabled)
}

// order is a topological sort; a cycle is an error.
func order(available map[string]*Module, enabled []string) ([]*Module, error) {
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[string]int{}
	var out []*Module
	var visit func(n string, path []string) error
	visit = func(n string, path []string) error {
		switch state[n] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("modules require each other in a cycle: %v", append(path, n))
		}
		state[n] = visiting
		reqs := append([]string(nil), available[n].Spec.Requires...)
		sort.Strings(reqs)
		for _, r := range reqs {
			if err := visit(r, append(path, n)); err != nil {
				return err
			}
		}
		state[n] = done
		out = append(out, available[n])
		return nil
	}
	sorted := append([]string(nil), enabled...)
	sort.Strings(sorted)
	for _, n := range sorted {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func names(m map[string]*Module) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
