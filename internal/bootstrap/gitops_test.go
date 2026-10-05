package bootstrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kushan94/bluepave/internal/config"
	"github.com/kushan94/bluepave/internal/run"
)

// The Argo CD install uses exactly what the gitops-argocd module renders, from the real charts.
func TestArgoCDApplicationFromTheRealCharts(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// The root chart reads .bluepave/discovered.yaml; use the test fixture's IDs.
	tmp := t.TempDir()
	for _, d := range []string{"platform", "modules"} {
		if err := os.CopyFS(filepath.Join(tmp, d), os.DirFS(filepath.Join(root, d))); err != nil {
			t.Fatal(err)
		}
	}
	for src, dst := range map[string]string{
		"bluepave.yaml":                        "bluepave.yaml",
		".bluepave/resolved.yaml":              ".bluepave/resolved.yaml",
		"platform/chart/tests/discovered.yaml": ".bluepave/discovered.yaml",
	} {
		data, err := os.ReadFile(filepath.Join(root, src))
		if err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Dir(filepath.Join(tmp, dst)), 0o755)
		if err := os.WriteFile(filepath.Join(tmp, dst), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b := Bootstrap{Runner: run.Exec{}, Platform: &config.Platform{}}
	app, err := b.argoCDApplication(context.Background(), tmp, "dev", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := app.Spec.Source
	if src.Chart != "argo-cd" || src.RepoURL != "https://argoproj.github.io/argo-helm" || src.TargetRevision == "" || src.Helm.ReleaseName != "argo-cd" {
		t.Errorf("source = %+v", src)
	}
	cm, _ := src.Helm.ValuesObject["configs"].(map[string]any)["cm"].(map[string]any)
	if cm["admin.enabled"] != false || cm["oidc.config"] == nil {
		t.Errorf("configs.cm = %v (want local admin off and Entra sign-in from the fixture's client ID)", cm)
	}
	if app.Spec.Destination.Namespace != "argocd" {
		t.Errorf("namespace = %q", app.Spec.Destination.Namespace)
	}
}
