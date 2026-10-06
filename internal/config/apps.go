package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
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
			Path    string `yaml:"path"`
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
		if a.Spec.Source.RepoURL != "" && !strings.HasPrefix(strings.ToLower(a.Spec.Source.RepoURL), strings.ToLower(owner)) {
			problems = append(problems, fmt.Errorf("%s: source.repoURL must be a repository of %s (the platform's GitHub App only reaches that owner)", f, owner))
		}
		// An app in this repository: its stage values files must be editable by Kargo.
		if a.Spec.Source.RepoURL == "" && a.Spec.Source.Path != "" {
			for _, st := range a.Spec.Stages {
				values := filepath.Join(root, filepath.Dir(a.Spec.Source.Path), "values-"+st.Name+".yaml")
				if err := checkStageValues(values); err != nil {
					problems = append(problems, fmt.Errorf("%s: %w", f, err))
				}
			}
		}
		apps = append(apps, &a)
	}
	return apps, errors.Join(problems...)
}

// checkStageValues checks a stage's values file, where Kargo writes images.<image>.repository,
// .tag and .digest on each promotion. Kargo's yaml-update edits block mappings only: on an inline
// one ({ repository: "", tag: "" }) it writes broken YAML, and the stage stops syncing. A missing
// file is fine (Kargo's first promotion creates the keys).
func checkStageValues(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "images" {
			continue
		}
		images := root.Content[i+1]
		if images.Style&yaml.FlowStyle != 0 {
			return fmt.Errorf("%s: images is an inline mapping; write it in block style (Kargo can't edit inline mappings)", path)
		}
		for j := 0; j+1 < len(images.Content); j += 2 {
			if images.Content[j+1].Style&yaml.FlowStyle != 0 {
				return fmt.Errorf("%s: images.%s is an inline mapping ({ ... }); write it in block style, one key per line (Kargo can't edit inline mappings)", path, images.Content[j].Value)
			}
		}
	}
	return nil
}
