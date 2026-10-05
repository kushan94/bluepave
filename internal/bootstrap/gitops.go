package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/kushan94/bluepave/internal/keyvault"
)

// GitOps hands each environment's cluster over to Argo CD: it installs Argo CD with the values the
// gitops-argocd module renders (so Argo CD adopts the install and manages itself from then on),
// gives it read access to the platform's repositories through the GitHub App, and applies the root
// Application. Argo CD reads bluepave.yaml and .bluepave/ from Git, so they must be pushed first.
func (b Bootstrap) GitOps(ctx context.Context, root string) error {
	if !b.enabled("gitops-argocd") {
		return errors.New("the gitops step needs the gitops-argocd module")
	}
	if err := b.pushed(ctx, root); err != nil {
		return err
	}
	for _, env := range b.Platform.Spec.Environments {
		if err := b.gitopsEnvironment(ctx, root, env); err != nil {
			return fmt.Errorf("environment %s: %w", env, err)
		}
	}
	return nil
}

// pushed checks that origin/main has the platform's configuration as it is here.
func (b Bootstrap) pushed(ctx context.Context, root string) error {
	paths := []string{"bluepave.yaml", ".bluepave", "apps"}
	if _, err := b.Runner.Run(ctx, "git", "-C", root, "fetch", "--quiet", "origin", "main"); err != nil {
		return err
	}
	untracked, err := b.Runner.Run(ctx, "git", append([]string{"-C", root, "ls-files", "--others", "--exclude-standard", "--"}, paths...)...)
	if err != nil {
		return err
	}
	_, diffErr := b.Runner.Run(ctx, "git", append([]string{"-C", root, "diff", "--quiet", "origin/main", "--"}, paths...)...)
	if diffErr != nil || strings.TrimSpace(string(untracked)) != "" {
		return fmt.Errorf("Argo CD reads %s from Git, and origin/main differs from this checkout: commit and push them to main, then run `bluepave up -step gitops`", strings.Join(paths, ", "))
	}
	return nil
}

func (b Bootstrap) gitopsEnvironment(ctx context.Context, root, env string) error {
	cluster, _ := b.IDs.Get("environments", env, "aks", "clusterName").(string)
	rg, _ := b.IDs.Get("environments", env, "aks", "resourceGroupName").(string)
	vault, _ := b.IDs.Get("environments", env, "keyvault", "keyVaultName").(string)
	if cluster == "" || rg == "" || vault == "" {
		return errors.New("no cluster or Key Vault recorded: run `bluepave up -step infra` first")
	}
	tmp, err := os.MkdirTemp("", "bluepave-gitops-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	b.logf("==> %s: cluster credentials (Microsoft Entra ID, in a temporary kubeconfig)", env)
	kubeconfig := filepath.Join(tmp, "kubeconfig")
	if _, err := b.Runner.Run(ctx, "az", "aks", "get-credentials", "--resource-group", rg, "--name", cluster,
		"--file", kubeconfig, "--overwrite-existing", "--output", "none"); err != nil {
		return err
	}
	if _, err := b.Runner.Run(ctx, "kubelogin", "convert-kubeconfig", "--login", "azurecli", "--kubeconfig", kubeconfig); err != nil {
		return err
	}
	kubectl := func(stdin []byte, args ...string) error {
		_, err := b.Runner.RunIn(ctx, stdin, "kubectl", append([]string{"--kubeconfig", kubeconfig}, args...)...)
		return err
	}

	b.logf("==> %s: Argo CD's access to the platform's repositories (GitHub App)", env)
	if err := kubectl(namespaceYAML("argocd"), "apply", "-f", "-"); err != nil {
		return err
	}
	creds, err := b.repoCredentials(ctx, keyvault.Client{Runner: b.Runner, Vault: vault})
	if err != nil {
		return err
	}
	if err := kubectl(creds, "apply", "-f", "-"); err != nil {
		return err
	}

	b.logf("==> %s: Argo CD (Helm, with the gitops-argocd module's values)", env)
	app, err := b.argoCDApplication(ctx, root, env, tmp)
	if err != nil {
		return err
	}
	valuesFile := filepath.Join(tmp, "argo-cd-values.yaml")
	values, err := yaml.Marshal(app.Spec.Source.Helm.ValuesObject)
	if err != nil {
		return err
	}
	if err := os.WriteFile(valuesFile, values, 0o600); err != nil {
		return err
	}
	if _, err := b.Runner.Run(ctx, "helm", "upgrade", "--install", app.Spec.Source.Helm.ReleaseName, app.Spec.Source.Chart,
		"--repo", app.Spec.Source.RepoURL, "--version", app.Spec.Source.TargetRevision,
		"--namespace", app.Spec.Destination.Namespace, "--create-namespace",
		"--values", valuesFile, "--kubeconfig", kubeconfig, "--wait", "--timeout", "10m"); err != nil {
		return err
	}

	b.logf("==> %s: root Application (Argo CD manages everything from here)", env)
	return kubectl(b.rootApplication(env), "apply", "-f", "-")
}

// repoCredentials is a credential template for every repository of the GitHub owner, from the
// GitHub App in Key Vault. gitops-argocd's ExternalSecret keeps an equivalent one in sync later;
// this one lets Argo CD read the platform repository before External Secrets runs.
func (b Bootstrap) repoCredentials(ctx context.Context, kv keyvault.Client) ([]byte, error) {
	get := func(name string) (string, error) {
		v, err := kv.Get(ctx, name)
		if err == nil && v == "" {
			err = fmt.Errorf("Key Vault %s has no %s: run `bluepave up -step github-app` first", kv.Vault, name)
		}
		return v, forbiddenHint(err)
	}
	appID, err := get(secretAppID)
	if err != nil {
		return nil, err
	}
	installation, err := get(secretInstallationID)
	if err != nil {
		return nil, err
	}
	key, err := get(secretPrivateKey)
	if err != nil {
		return nil, err
	}
	secret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "bootstrap-github-app",
			"namespace": "argocd",
			"labels":    map[string]string{"argocd.argoproj.io/secret-type": "repo-creds"},
		},
		"stringData": map[string]string{
			"type":                    "git",
			"url":                     "https://github.com/" + b.Platform.Spec.GitHub.Owner,
			"githubAppID":             appID,
			"githubAppInstallationID": installation,
			"githubAppPrivateKey":     key,
		},
	}
	return yaml.Marshal(secret)
}

type argoApplication struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Source struct {
			RepoURL        string `yaml:"repoURL"`
			Chart          string `yaml:"chart"`
			Path           string `yaml:"path"`
			TargetRevision string `yaml:"targetRevision"`
			Helm           struct {
				ReleaseName  string         `yaml:"releaseName"`
				ValuesObject map[string]any `yaml:"valuesObject"`
			} `yaml:"helm"`
		} `yaml:"source"`
		Destination struct {
			Namespace string `yaml:"namespace"`
		} `yaml:"destination"`
	} `yaml:"spec"`
}

// argoCDApplication renders the root chart, then the gitops-argocd module's chart with the values
// the root gives it, and returns the module's argo-cd Application: the chart, version and values
// Argo CD will manage itself with.
func (b Bootstrap) argoCDApplication(ctx context.Context, root, env, tmp string) (*argoApplication, error) {
	rootOut, err := b.Runner.Run(ctx, "helm", "template", "root", filepath.Join(root, "platform/chart"),
		"--namespace", "argocd",
		"--values", filepath.Join(root, "bluepave.yaml"),
		"--values", filepath.Join(root, ".bluepave/resolved.yaml"),
		"--values", filepath.Join(root, ".bluepave/discovered.yaml"),
		"--set", "environment="+env)
	if err != nil {
		return nil, err
	}
	module, err := findApplication(rootOut, "module-gitops-argocd")
	if err != nil {
		return nil, err
	}
	moduleValues := filepath.Join(tmp, "module-values.yaml")
	data, err := yaml.Marshal(module.Spec.Source.Helm.ValuesObject)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(moduleValues, data, 0o600); err != nil {
		return nil, err
	}
	moduleOut, err := b.Runner.Run(ctx, "helm", "template", "module", filepath.Join(root, module.Spec.Source.Path),
		"--namespace", "argocd", "--values", moduleValues)
	if err != nil {
		return nil, err
	}
	return findApplication(moduleOut, "argo-cd")
}

// findApplication returns the Argo CD Application with this name from rendered manifests.
func findApplication(manifests []byte, name string) (*argoApplication, error) {
	dec := yaml.NewDecoder(bytes.NewReader(manifests))
	for {
		var app argoApplication
		err := dec.Decode(&app)
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("rendered manifests have no Application %q", name)
		}
		if err != nil {
			return nil, err
		}
		if app.Kind == "Application" && app.Metadata.Name == name {
			return &app, nil
		}
	}
}

// rootApplication renders the root chart from the platform repository for one environment
// (ADR-0002).
func (b Bootstrap) rootApplication(env string) []byte {
	gh := b.Platform.Spec.GitHub
	app := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata":   map[string]any{"name": "root", "namespace": "argocd"},
		"spec": map[string]any{
			"project": "default",
			"source": map[string]any{
				"repoURL":        "https://github.com/" + gh.Owner + "/" + gh.PlatformRepo + ".git",
				"targetRevision": "main",
				"path":           "platform/chart",
				"helm": map[string]any{
					"releaseName":  "root",
					"valueFiles":   []string{"../../bluepave.yaml", "../../.bluepave/resolved.yaml", "../../.bluepave/discovered.yaml"},
					"valuesObject": map[string]any{"environment": env},
				},
			},
			"destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": "argocd"},
			"syncPolicy":  map[string]any{"automated": map[string]any{"prune": true, "selfHeal": true}},
		},
	}
	out, _ := yaml.Marshal(app)
	return out
}

func namespaceYAML(name string) []byte {
	return []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + name + "\n")
}
