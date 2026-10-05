package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// App is an onboarding file, apps/<name>.yaml. Only the fields the CLI checks are typed.
type App struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Stages []struct {
			Name        string `yaml:"name"`
			Environment string `yaml:"environment"`
		} `yaml:"stages"`
		Source struct {
			RepoURL string `yaml:"repoURL"`
		} `yaml:"source"`
	} `yaml:"spec"`
}

// LoadApps reads and checks every apps/*.yaml under root against the app schema and the platform:
// the file is named after the app, stage names are unique, stages run on the platform's
// environments, and the repository belongs to the platform's GitHub owner (the platform's GitHub
// App only reaches that owner). It returns all problems, not just the first.
func LoadApps(root string, p *Platform) ([]*App, error) {
	files, err := filepath.Glob(filepath.Join(root, "apps", "*.yaml"))
	if err != nil {
		return nil, err
	}
	var apps []*App
	var problems []error
	for _, f := range files {
		var a App
		if err := loadValidated(f, "app.schema.json", &a); err != nil {
			problems = append(problems, err)
			continue
		}
		if want := strings.TrimSuffix(filepath.Base(f), ".yaml"); a.Metadata.Name != want {
			problems = append(problems, fmt.Errorf("%s: metadata.name is %q, want %q (the file name)", f, a.Metadata.Name, want))
		}
		seen := map[string]bool{}
		for _, s := range a.Spec.Stages {
			if seen[s.Name] {
				problems = append(problems, fmt.Errorf("%s: stage %q appears twice", f, s.Name))
			}
			seen[s.Name] = true
			env := s.Environment
			if env == "" {
				env = "dev"
			}
			if !slices.Contains(p.Spec.Environments, env) {
				problems = append(problems, fmt.Errorf("%s: stage %q runs on environment %q, which bluepave.yaml doesn't have (%v)", f, s.Name, env, p.Spec.Environments))
			}
		}
		owner := "https://github.com/" + p.Spec.GitHub.Owner + "/"
		if !strings.HasPrefix(strings.ToLower(a.Spec.Source.RepoURL), strings.ToLower(owner)) {
			problems = append(problems, fmt.Errorf("%s: source.repoURL must be a repository of %s (the platform's GitHub App only reaches that owner)", f, owner))
		}
		apps = append(apps, &a)
	}
	return apps, errors.Join(problems...)
}
