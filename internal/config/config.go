// Package config loads and validates a platform configuration (bluepave.yaml) together with its
// profile (ADR-0001).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"go.yaml.in/yaml/v3"

	v1alpha1 "github.com/kushan94/bluepave/api/v1alpha1"
	"github.com/kushan94/bluepave/internal/schema"
)

// Platform is bluepave.yaml. Only the fields the CLI acts on are typed; the schema checks the rest.
type Platform struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Prefix  string `yaml:"prefix"`
		Profile string `yaml:"profile"`
		Azure   struct {
			Region string `yaml:"region"`
		} `yaml:"azure"`
		Environments []string `yaml:"environments"`
		DNS          struct {
			Domain string `yaml:"domain"`
		} `yaml:"dns"`
		Admins struct {
			Group string `yaml:"group"`
		} `yaml:"admins"`
		GitHub struct {
			Owner        string `yaml:"owner"`
			PlatformRepo string `yaml:"platformRepo"`
			AppName      string `yaml:"appName"`
		} `yaml:"github"`
		Modules map[string]ModuleOverride `yaml:"modules"`
	} `yaml:"spec"`
}

// ModuleOverride turns a module on or off and overrides its settings.
type ModuleOverride struct {
	Enabled  *bool          `yaml:"enabled"`
	Settings map[string]any `yaml:"settings"`
}

// Profile is profiles/<name>.yaml.
type Profile struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		KeyVault struct {
			PurgeProtection bool `yaml:"purgeProtection"`
		} `yaml:"keyVault"`
		Modules struct {
			DefaultsEnabled []string `yaml:"defaultsEnabled"`
		} `yaml:"modules"`
	} `yaml:"spec"`
}

// LoadPlatform reads and validates bluepave.yaml.
func LoadPlatform(path string) (*Platform, error) {
	var p Platform
	if err := loadValidated(path, "platform.schema.json", &p); err != nil {
		return nil, err
	}
	if len(p.Spec.Environments) == 0 {
		p.Spec.Environments = []string{"dev"}
	}
	return &p, nil
}

// LoadProfile reads and validates profiles/<name>.yaml under root.
func LoadProfile(root, name string) (*Profile, error) {
	var p Profile
	path := filepath.Join(root, "profiles", name+".yaml")
	if err := loadValidated(path, "profile.schema.json", &p); err != nil {
		return nil, err
	}
	if p.Metadata.Name != name {
		return nil, fmt.Errorf("%s: metadata.name is %q, want %q", path, p.Metadata.Name, name)
	}
	return &p, nil
}

// EnabledModules returns the modules on for this platform: the profile's defaults, with the
// platform's overrides applied, in a stable order.
func EnabledModules(p *Platform, prof *Profile) []string {
	on := map[string]bool{}
	var order []string
	add := func(n string) {
		if !on[n] {
			on[n] = true
			order = append(order, n)
		}
	}
	for _, n := range prof.Spec.Modules.DefaultsEnabled {
		add(n)
	}
	names := make([]string, 0, len(p.Spec.Modules))
	for n := range p.Spec.Modules {
		names = append(names, n)
	}
	sort.Strings(names) // map order is random; the result must not be
	for _, n := range names {
		o := p.Spec.Modules[n]
		if o.Enabled == nil {
			continue
		}
		if *o.Enabled {
			add(n)
		} else {
			on[n] = false
		}
	}
	var out []string
	for _, n := range order {
		if on[n] {
			out = append(out, n)
		}
	}
	return out
}

func loadValidated(path, schemaName string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	v, err := schema.Load(v1alpha1.Schemas, schemaName)
	if err != nil {
		return err
	}
	if err := v.ValidateYAML(data); err != nil {
		return fmt.Errorf("%s is not a valid %s document:\n%w", path, schemaName, err)
	}
	return yaml.Unmarshal(data, into)
}
