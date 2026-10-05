package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func platformFor(owner string, envs ...string) *Platform {
	p := &Platform{}
	p.Spec.GitHub.Owner = owner
	p.Spec.Environments = envs
	return p
}

func TestLoadApps(t *testing.T) {
	apps, err := LoadApps("testdata/apps/good", platformFor("acme", "dev"))
	if err != nil {
		t.Fatalf("LoadApps: %v", err)
	}
	if len(apps) != 1 || apps[0].Metadata.Name != "greeter" {
		t.Fatalf("apps = %v", apps)
	}
}

func TestLoadAppsProblems(t *testing.T) {
	_, err := LoadApps("testdata/apps/bad", platformFor("acme", "dev"))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{
		`metadata.name is "other", want "misnamed"`,
		`stage "dev" appears twice`,
		`stage "live" runs on environment "prod", which bluepave.yaml doesn't have`,
		`source.repoURL must be a repository of https://github.com/acme/`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}

// The onboarding chart's test fixtures must be valid app files too.
func TestOnboardingFixturesAreValidApps(t *testing.T) {
	files, err := filepath.Glob("../../platform/charts/app-onboarding/tests/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		var a App
		if err := loadValidated(f, "app.schema.json", &a); err != nil {
			t.Errorf("%v", err)
		}
	}
}
