package modules

import (
	"strings"
	"testing"
)

func TestDiscoverAndResolve(t *testing.T) {
	avail, err := Discover("testdata/good")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(avail) != 2 {
		t.Fatalf("found %d modules, want 2", len(avail))
	}
	ordered, err := Resolve(avail, []string{"argocd", "aks"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if s := avail["aks"].Spec.InfraScope; s != "environment" {
		t.Errorf("infraScope default = %q, want environment", s)
	}
	if got := ordered[0].Metadata.Name + "," + ordered[1].Metadata.Name; got != "aks,argocd" {
		t.Errorf("install order = %s, want aks,argocd (requirements first)", got)
	}
}

func TestResolveErrors(t *testing.T) {
	avail, _ := Discover("testdata/good")
	for name, tc := range map[string]struct {
		enabled []string
		want    string
	}{
		"missing requirement": {[]string{"argocd"}, `requires "aks", which isn't enabled`},
		"unknown module":      {[]string{"aks", "nope"}, `module "nope" is enabled but doesn't exist`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(avail, tc.enabled); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestResolveCycle(t *testing.T) {
	avail, err := Discover("testdata/cycle")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if _, err := Resolve(avail, []string{"alpha", "beta"}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("err = %v, want a cycle error", err)
	}
}

func TestDiscoverReportsEveryBrokenManifest(t *testing.T) {
	_, err := Discover("testdata/broken")
	if err == nil {
		t.Fatal("Discover: want errors for the broken fixtures")
	}
	for _, want := range []string{
		"badversion/module.yaml is not a valid module manifest",
		`spec.layers.infra points to "nope.bicep"`,
		`metadata.name "othername" must match its folder "wrongname"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %q:\n%v", want, err)
		}
	}
}

// after orders a module behind another only when that one is enabled, and doesn't require it.
func TestResolveAfter(t *testing.T) {
	avail, err := Discover("testdata/after")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	ordered, err := Resolve(avail, []string{"alpha", "zeta"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := ordered[0].Metadata.Name + "," + ordered[1].Metadata.Name; got != "zeta,alpha" {
		t.Errorf("install order = %s, want zeta,alpha", got)
	}
	ordered, err = Resolve(avail, []string{"alpha"})
	if err != nil {
		t.Fatalf("Resolve without zeta: %v", err)
	}
	if len(ordered) != 1 || ordered[0].Metadata.Name != "alpha" {
		t.Errorf("without zeta: got %d modules, want only alpha", len(ordered))
	}
}
