package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/kushan94/bluepave/internal/bootstrap"
	"github.com/kushan94/bluepave/internal/githubapp"

	"github.com/kushan94/bluepave/internal/discovered"
	cmdrun "github.com/kushan94/bluepave/internal/run"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No test may reach a real browser or GitHub: the github-app step's flow fails unless a test
// installs its own.
func TestMain(m *testing.M) {
	appFlow = bootstrap.AppFlow{
		OpenURL: func(string) {},
		NewCode: func(context.Context, githubapp.Flow) (string, error) {
			return "", errors.New("tests must not start the GitHub App flow")
		},
		HTTP:    &http.Client{},
		APIBase: "http://127.0.0.1:1",
		Poll:    time.Millisecond,
		Timeout: time.Second,
	}
	os.Exit(m.Run())
}

func TestValidateExample(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "-f", exampleConfig, "-root", exampleRoot(t)}, &out, &errOut)
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
	// The real bluepave.yaml here: CI fails until .bluepave/ is re-rendered and committed.
	if code := run([]string{"render", "-check", "-f", "../../bluepave.yaml", "-root", "../.."}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
}

func TestRender(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "resolved.yaml")
	var out, errOut bytes.Buffer
	if code := run([]string{"render", "-f", exampleConfig, "-root", exampleRoot(t), "-o", dst}, &out, &errOut); code != 0 {
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
	// The platform-settings entity, next to it.
	data, err = os.ReadFile(filepath.Join(filepath.Dir(dst), "platform-settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"kind: Resource",
		"name: platform-settings",
		"bluepave.dev/github-owner: acme",
		"bluepave.dev/platform-repo: acme-platform",
		"bluepave.dev/revision: main",
		"bluepave.dev/environment: dev",
		"bluepave.dev/domain: dev.",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("platform-settings.yaml lacks %q:\n%s", want, data)
		}
	}
}

func TestSettingsChecked(t *testing.T) {
	base, err := os.ReadFile(exampleConfig)
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
			if code := run([]string{"validate", "-f", f, "-root", exampleRoot(t)}, &out, &errOut); code != 1 {
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
	if code := run([]string{"validate", "-f", exampleConfig, "-root", root}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1; stdout: %s", code, out.String())
	}
	if want := `both claim the platform hostname "argocd"`; !strings.Contains(errOut.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", errOut.String(), want)
	}
}

func TestPlan(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "-f", exampleConfig, "-root", exampleRoot(t)}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"1. dns", "bp-acme-dns", "dev/aks", "bp-acme-dev-aks"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan lacks %q:\n%s", want, out.String())
		}
	}
}

// up against a recording runner: the account is recorded, every stack is created in plan order,
// and the outputs land in discovered.yaml.
func TestUpInfra(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"profiles", "modules"} {
		if err := os.CopyFS(filepath.Join(root, d), os.DirFS(filepath.Join("../..", d))); err != nil {
			t.Fatal(err)
		}
	}
	stacks := &cmdrun.FakeStacks{Default: `{"outputs": {"marker": {"type": "String", "value": "ok"}}}`}
	rec := &cmdrun.Recorder{Responses: preflightResponses(t, map[string]string{
		"az account show": `{"id": "sub-1", "tenantId": "tenant-1"}`,
		"az bicep build":  `{"parameters": {"environmentName": {}}}`,
	}), Handlers: []func(string) (string, bool){stacks.Handle}}
	old := runner
	runner = rec
	defer func() { runner = old }()

	var out, errOut bytes.Buffer
	code := run([]string{"up", "-step", "infra", "-f", exampleConfig, "-root", root, "-yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var creates []string
	for _, c := range rec.Calls {
		if strings.HasPrefix(c.String(), "az stack sub create") {
			creates = append(creates, c.Args[4]) // --name <stack>
		}
	}
	if len(creates) == 0 || creates[0] != "bp-acme-dns" || creates[len(creates)-1] != "bp-acme-dev-self-service" {
		t.Errorf("stacks created: %v", creates)
	}
	ids, err := discovered.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if ids.Get("azure", "subscriptionId") != "sub-1" || ids.Get("environments", "dev", "aks", "marker") != "ok" {
		t.Errorf("discovered = %v", ids)
	}
}

// The whole of `up` against a recording runner: accounts, infra, identities. Every Entra object is
// new; Key Vault has no secrets yet.
func TestUpAll(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"profiles", "modules"} {
		if err := os.CopyFS(filepath.Join(root, d), os.DirFS(filepath.Join("../..", d))); err != nil {
			t.Fatal(err)
		}
	}
	stacks := &cmdrun.FakeStacks{Default: `{"outputs": {
			"oidcIssuerUrl": {"value": "https://issuer.example/"},
			"keyVaultName": {"value": "kv-1"},
			"containerRegistryLoginServer": {"value": "cr1.azurecr.io"},
			"clusterName": {"value": "aks-1"},
			"resourceGroupName": {"value": "rg-1"}}}`}
	const portalSecret = "s3cret-from-entra"
	rec := &cmdrun.Recorder{Responses: preflightResponses(t, map[string]string{
		"az account show":                     `{"id": "sub-1", "tenantId": "tenant-1"}`,
		"az ad signed-in-user show":           "user-1",
		"az ad group create":                  "group-1",
		"az ad group member check":            "false",
		"az ad app create":                    "app-1",
		"az ad sp create":                     "sp-1",
		"az ad app credential reset":          portalSecret,
		"az ad app federated-credential list": "null",
		"gh api repos/acme/acme-platform/environments/dev/deployment-branch-policies": "0",
		"az keyvault secret list": "0",
		"az bicep build":          `{"parameters": {"environmentName": {}}}`,
		// Every stack returns every output the identities step reads.
	}), Handlers: []func(string) (string, bool){stacks.Handle}}
	old := runner
	runner = rec
	defer func() { runner = old }()

	// The GitHub App: a fake browser flow, a real key, and a stand-in for GitHub's API.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	conversion, _ := json.Marshal(map[string]any{"id": 77, "slug": "acme-acme-platform", "client_id": "Iv1.test", "pem": keyPEM})
	rec.Responses["gh api --method POST /app-manifests/code-1/conversions"] = string(conversion)
	rec.Responses["gh api users/acme"] = "User"
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 4242, "account": {"login": "acme"}}]`))
	}))
	defer gh.Close()
	// gitops: the checkout matches origin/main (git diff succeeds), and the charts render (canned).
	rec.Responses["helm template root"] = `---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: module-gitops-argocd}
spec: {source: {path: modules/gitops-argocd/gitops, helm: {valuesObject: {bluepave: {environment: dev}}}}}
`
	rec.Responses["helm template module"] = `---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: argo-cd}
spec:
  source: {repoURL: https://argoproj.github.io/argo-helm, chart: argo-cd, targetRevision: 10.9.6, helm: {releaseName: argo-cd, valuesObject: {dex: {enabled: false}}}}
  destination: {namespace: argocd}
`
	rec.Responses["az keyvault secret show"] = "from-key-vault"
	oldFlow := appFlow
	appFlow = bootstrap.AppFlow{
		OpenURL: func(string) {},
		NewCode: func(context.Context, githubapp.Flow) (string, error) { return "code-1", nil },
		HTTP:    gh.Client(), APIBase: gh.URL, Poll: time.Millisecond, Timeout: 5 * time.Second,
	}
	defer func() { appFlow = oldFlow }()

	var out, errOut bytes.Buffer
	if code := run([]string{"up", "-f", exampleConfig, "-root", root, "-yes"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	var lines []string
	for _, c := range rec.Calls {
		line := c.String()
		if strings.Contains(line, portalSecret) || strings.Contains(line, "PRIVATE KEY") {
			t.Errorf("a secret reached a command line: %s", line)
		}
		lines = append(lines, line)
	}
	all := strings.Join(lines, "\n")
	// Secrets reach kubectl on stdin; the root Application is applied last.
	var applied []string
	for _, c := range rec.Calls {
		if c.Name == "kubectl" && len(c.Stdin) > 0 {
			applied = append(applied, string(c.Stdin))
		}
	}
	if len(applied) != 3 || !strings.Contains(applied[1], "githubAppPrivateKey: from-key-vault") || !strings.Contains(applied[2], "path: platform/chart") {
		t.Errorf("kubectl stdin documents = %q", applied)
	}
	for _, want := range []string{
		"az ad group member add --group group-1 --member-id user-1",
		`"subject":"repo:acme/acme-platform:environment:dev"`,
		`"subject":"system:serviceaccount:argocd:argocd-server"`,
		`"subject":"system:serviceaccount:observability:grafana"`,
		"az keyvault secret set --vault-name kv-1 --name portal-entra-client-secret --file",
		"az keyvault secret set --vault-name kv-1 --name kargo-admin-password-hash --file",
		"gh variable set BLUEPAVE_CLIENT_ID --repo acme/acme-platform --body app-1",
		"gh variable set BLUEPAVE_REGISTRY --repo acme/acme-platform --body cr1.azurecr.io",
		"az keyvault secret set --vault-name kv-1 --name github-app-private-key --file",
		"az keyvault secret set --vault-name kv-1 --name github-app-installation-id --file",
		"helm upgrade --install argo-cd argo-cd --repo https://argoproj.github.io/argo-helm --version 10.9.6 --namespace argocd",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no command contains %q", want)
		}
	}
	// accounts runs before infra: the admins group and CI identity exist when the registry and Key
	// Vault grant them roles.
	if strings.Index(all, "az ad group create") > strings.Index(all, "az stack sub create") {
		t.Error("the admins group was created after the first stack")
	}
	ids, err := discovered.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"admins/groupObjectId":                    "group-1",
		"ci/principalId":                          "sp-1",
		"environments/dev/gitops-argocd/clientId": "app-1",
		"environments/dev/portal/appClientId":     "app-1",
		"github/appId":                            "77",
		"github/installationId":                   "4242",
	} {
		if got := ids.Get(strings.Split(path, "/")...); got != want {
			t.Errorf("%s = %v, want %s", path, got, want)
		}
	}
}

func TestStatus(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"profiles", "modules"} {
		if err := os.CopyFS(filepath.Join(root, d), os.DirFS(filepath.Join("../..", d))); err != nil {
			t.Fatal(err)
		}
	}
	ids := discovered.IDs{}
	ids.SetOutputs("dev", "aks", map[string]any{"clusterName": "aks-1", "resourceGroupName": "rg-1"})
	ids.SetOutputs("", "dns", map[string]any{"nameServers": []any{"ns1.example."}})
	if err := ids.Save(root); err != nil {
		t.Fatal(err)
	}
	rec := &cmdrun.Recorder{Responses: map[string]string{
		"az stack sub show": "succeeded",
		"kubectl": `{"items": [
			{"metadata": {"name": "root"}, "status": {"sync": {"status": "Synced"}, "health": {"status": "Healthy"}}},
			{"metadata": {"name": "kargo"}, "status": {"sync": {"status": "OutOfSync"}, "health": {"status": "Progressing"}}}]}`,
	}}
	old := runner
	runner = rec
	defer func() { runner = old }()
	var out, errOut bytes.Buffer
	code := run([]string{"status", "-f", exampleConfig, "-root", root}, &out, &errOut)
	if code != 1 { // kargo isn't synced
		t.Errorf("exit %d, want 1 (an application isn't healthy)", code)
	}
	for _, want := range []string{"dev/aks", "succeeded", "kargo", "OutOfSync", "https://portal.dev.platform.example.com", "ns1.example."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out.String())
		}
	}
}

func TestDown(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"profiles", "modules"} {
		if err := os.CopyFS(filepath.Join(root, d), os.DirFS(filepath.Join("../..", d))); err != nil {
			t.Fatal(err)
		}
	}
	ids := discovered.IDs{}
	ids.Set("sub-1", "azure", "subscriptionId")
	ids.SetOutputs("dev", "keyvault", map[string]any{"keyVaultName": "kv-1"})
	if err := ids.Save(root); err != nil {
		t.Fatal(err)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	deletedStacks := map[string]bool{}
	rec := &cmdrun.Recorder{Responses: map[string]string{
		"az account show":         `{"id": "sub-1", "tenantId": "tenant-1"}`,
		"az keyvault secret list": "1",
		"az keyvault secret show --vault-name kv-1 --name github-app-private-key": keyPEM,
		"az keyvault secret show --vault-name kv-1 --name github-app-id":          "77",
		"az ad app list":           "app-x",
		"az keyvault list-deleted": "1",
		// BLUEPAVE_REGISTRY was never set: only the others are deleted.
		"gh variable list": "BLUEPAVE_TENANT_ID\nBLUEPAVE_SUBSCRIPTION_ID\nBLUEPAVE_CLIENT_ID\n",
	}, Handlers: []func(string) (string, bool){func(line string) (string, bool) {
		// Every stack is deployed until down deletes it.
		switch {
		case strings.HasPrefix(line, "az stack sub delete"):
			deletedStacks[strings.Fields(line)[5]] = true
			return "", true
		case strings.HasPrefix(line, "az stack sub list"):
			name := line[strings.Index(line, "[?name=='")+len("[?name=='"):]
			if deletedStacks[name[:strings.Index(name, "'")]] {
				return "", true
			}
			return "succeeded", true
		}
		return "", false
	}}}
	deleted := false
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/app" && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer gh.Close()
	oldRunner, oldFlow := runner, appFlow
	runner = rec
	appFlow = bootstrap.AppFlow{HTTP: gh.Client(), APIBase: gh.URL}
	defer func() { runner, appFlow = oldRunner, oldFlow }()

	var out, errOut bytes.Buffer
	if code := run([]string{"down", "-f", exampleConfig, "-root", root, "-yes"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	if !deleted {
		t.Error("the GitHub App wasn't deleted")
	}
	var stackDeletes []string
	all := ""
	for _, c := range rec.Calls {
		all += c.String() + "\n"
		if strings.HasPrefix(c.String(), "az stack sub delete") {
			stackDeletes = append(stackDeletes, c.Args[4])
		}
	}
	// Reverse order: the last environment module first, the global modules last.
	if len(stackDeletes) == 0 || stackDeletes[0] != "bp-acme-dev-self-service" || stackDeletes[len(stackDeletes)-1] != "bp-acme-dns" {
		t.Errorf("stack deletions: %v", stackDeletes)
	}
	for _, want := range []string{"az ad app delete --id app-x", "az keyvault purge --name kv-1", "gh variable delete BLUEPAVE_CLIENT_ID"} {
		if !strings.Contains(all, want) {
			t.Errorf("no command %q", want)
		}
	}
	if strings.Contains(all, "BEGIN RSA") {
		t.Error("the App's key reached a command line")
	}
	back, _ := discovered.Load(root)
	if len(back) != 0 {
		t.Errorf("discovered not reset: %v", back)
	}
}

// preflightResponses answers preflight's az calls: the trial profile fits (preflight's fixtures).
func preflightResponses(t *testing.T, responses map[string]string) map[string]string {
	t.Helper()
	for prefix, file := range map[string]string{
		"az rest --method get --url https://management.azure.com/subscriptions/sub-1/providers/Microsoft.Compute/skus": "skus-ok.json",
		"az vm list-usage":       "usage-free.json",
		"az network list-usages": "network-free.json",
		"az rest --method get --url https://management.azure.com/subscriptions/sub-1/providers/Microsoft.DBforPostgreSQL/locations": "postgres.json",
	} {
		b, err := os.ReadFile(filepath.Join("../../internal/preflight/testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		responses[prefix] = string(b)
	}
	return responses
}

// exampleConfig is the template's example platform (testdata), so the tests don't depend on what
// a platform repository has made of its own bluepave.yaml.
const exampleConfig = "testdata/bluepave.yaml"

// exampleRoot is a repository root with this checkout's profiles and modules, and none of its
// apps (which name the platform's own repositories, not the example's).
func exampleRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"profiles", "modules"} {
		if err := os.CopyFS(filepath.Join(root, d), os.DirFS(filepath.Join("../..", d))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
