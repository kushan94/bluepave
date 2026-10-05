package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `apiVersion: bluepave.dev/v1alpha1
kind: Platform
metadata: {name: acme-platform}
spec:
  prefix: acme
  profile: trial
  azure: {region: westeurope}
  dns: {domain: platform.example.com}
  github: {owner: acme, platformRepo: acme-platform}
  admins: {group: acme-admins}
`

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bluepave.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadPlatformValid(t *testing.T) {
	p, err := LoadPlatform(write(t, valid))
	if err != nil {
		t.Fatalf("LoadPlatform: %v", err)
	}
	if p.Spec.Prefix != "acme" || p.Spec.Azure.Region != "westeurope" {
		t.Errorf("parsed %+v", p.Spec)
	}
	if len(p.Spec.Environments) != 1 || p.Spec.Environments[0] != "dev" {
		t.Errorf("environments = %v, want the default [dev]", p.Spec.Environments)
	}
}

func TestLoadPlatformInvalid(t *testing.T) {
	for name, tc := range map[string]struct{ from, to, want string }{
		"prefix too long":  {"prefix: acme", "prefix: acmecorp", "/spec/prefix"},
		"unknown profile":  {"profile: trial", "profile: huge", "/spec/profile"},
		"unknown field":    {"prefix: acme", "prefix: acme\n  colour: blue", "colour"},
		"bad domain":       {"domain: platform.example.com", "domain: not_a_domain", "/spec/dns/domain"},
		"missing required": {"  admins: {group: acme-admins}\n", "", "admins"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadPlatform(write(t, strings.Replace(valid, tc.from, tc.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestEnabledModules(t *testing.T) {
	p, err := LoadPlatform(write(t, valid+"  modules:\n    portal: {enabled: false}\n    extra: {enabled: true}\n    aks: {settings: {x: 1}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	prof := &Profile{}
	prof.Spec.Modules.DefaultsEnabled = []string{"aks", "portal"}
	got := strings.Join(EnabledModules(p, prof), ",")
	if got != "aks,extra" {
		t.Errorf("enabled = %s, want aks,extra (portal turned off, extra turned on, settings-only override keeps the default)", got)
	}
}

func TestLoadProfile(t *testing.T) {
	if _, err := LoadProfile("testdata", "trial"); err != nil {
		t.Errorf("LoadProfile(trial): %v", err)
	}
	if _, err := LoadProfile("testdata", "standard"); err == nil {
		t.Error("LoadProfile(standard): want an error, there's no such file in testdata")
	}
}
