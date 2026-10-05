package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExample(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "-f", "../../bluepave.yaml", "-root", "../.."}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `platform "acme-platform", profile trial`) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"deploy"}, &out, &errOut); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

// The committed .bluepave/resolved.yaml must match bluepave.yaml (CI runs the same check).
func TestResolvedIsCurrent(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"render", "-check", "-f", "../../bluepave.yaml", "-root", "../.."}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
}

func TestRender(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "resolved.yaml")
	var out, errOut bytes.Buffer
	if code := run([]string{"render", "-f", "../../bluepave.yaml", "-root", "../..", "-o", dst}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"profile: trial",
		"gitops: modules/gitops-argocd/gitops",
		"url: https://argoproj.github.io/argo-helm",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("resolved.yaml lacks %q:\n%s", want, data)
		}
	}
}

func TestSettingsChecked(t *testing.T) {
	base, err := os.ReadFile("../../bluepave.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ modules, want string }{
		"against the module's schema": {
			"    aks: {settings: {maintenanceWindow: {dayOfWeek: Funday}}}\n",
			"spec.modules.aks.settings:\n/maintenanceWindow/dayOfWeek",
		},
		"module without settings": {
			"    monitoring: {settings: {x: 1}}\n",
			`module "monitoring" takes no settings`,
		},
		"module not enabled": {
			"    runtime-nope: {settings: {x: 1}}\n",
			"spec.modules.runtime-nope has settings, but the module isn't enabled",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "bluepave.yaml")
			if err := os.WriteFile(f, append(append([]byte{}, base...), tc.modules...), 0o644); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			if code := run([]string{"validate", "-f", f, "-root", "../.."}, &out, &errOut); code != 1 {
				t.Fatalf("exit %d, want 1; stdout: %s", code, out.String())
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tc.want)
			}
		})
	}
}

func TestHostnameClaimedTwice(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"profiles", "modules"} {
		if err := os.CopyFS(filepath.Join(root, d), os.DirFS(filepath.Join("../..", d))); err != nil {
			t.Fatal(err)
		}
	}
	// A second module claiming Argo CD's hostname.
	twin := filepath.Join(root, "modules", "rollouts", "module.yaml")
	data, err := os.ReadFile(twin)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "  layers:", "  hostnames:\n    - { name: argocd, namespace: argo-rollouts }\n  layers:", 1))
	if err := os.WriteFile(twin, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"validate", "-f", "../../bluepave.yaml", "-root", root}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1; stdout: %s", code, out.String())
	}
	if want := `both claim the platform hostname "argocd"`; !strings.Contains(errOut.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", errOut.String(), want)
	}
}
