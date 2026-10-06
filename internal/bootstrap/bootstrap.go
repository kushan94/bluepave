// Package bootstrap creates what modules can't create for themselves, around the infra
// deployment (`bluepave up`):
//
//	accounts   (before infra) resource providers, the admins group, the CI identity trusted by the
//	           platform repository's GitHub environments, main-only environments, repository
//	           variables
//	identities (after infra)  Entra apps for Argo CD, Grafana and the portal (federated to the
//	           cluster's OIDC issuer: no client secrets except the portal's, which goes to Key
//	           Vault), Kargo's admin credentials, the registry variable
//
// Everything converges: running a step again changes only what's missing or wrong. IDs go to
// .bluepave/discovered.yaml; secrets go to Key Vault and are never printed.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/kushan94/bluepave/internal/config"
	"github.com/kushan94/bluepave/internal/discovered"
	"github.com/kushan94/bluepave/internal/entra"
	"github.com/kushan94/bluepave/internal/github"
	"github.com/kushan94/bluepave/internal/keyvault"
	"github.com/kushan94/bluepave/internal/run"
)

// Providers are the Azure resource providers the modules use.
var Providers = []string{
	"Microsoft.Compute", "Microsoft.ContainerService", "Microsoft.ContainerRegistry", "Microsoft.KeyVault",
	"Microsoft.Network", "Microsoft.OperationalInsights", "Microsoft.Insights", "Microsoft.Monitor",
	"Microsoft.AlertsManagement", "Microsoft.PolicyInsights", "Microsoft.Consumption",
	"Microsoft.ManagedIdentity", "Microsoft.DBforPostgreSQL", "Microsoft.Storage", "Microsoft.Cache",
}

// Bootstrap holds what both steps need.
type Bootstrap struct {
	Runner   run.Runner
	Platform *config.Platform
	Modules  []string // enabled modules
	IDs      discovered.IDs
	Log      io.Writer
	// PollInterval is how often waits poll Azure (default 30s; tests set it short).
	PollInterval time.Duration
}

func (b Bootstrap) repo() string {
	return b.Platform.Spec.GitHub.Owner + "/" + b.Platform.Spec.GitHub.PlatformRepo
}

func (b Bootstrap) enabled(m string) bool { return slices.Contains(b.Modules, m) }

func (b Bootstrap) logf(format string, args ...any) { fmt.Fprintf(b.Log, format+"\n", args...) }

// Accounts runs the step before infra.
func (b Bootstrap) Accounts(ctx context.Context) error {
	ad := entra.Client{Runner: b.Runner}
	gh := github.Client{Runner: b.Runner}
	name := b.Platform.Metadata.Name

	b.logf("==> Resource providers")
	for _, ns := range Providers {
		// Registration continues in the background; deployments that need it wait for it.
		if _, err := b.Runner.Run(ctx, "az", "provider", "register", "--namespace", ns, "--output", "none"); err != nil {
			return err
		}
	}

	b.logf("==> Admins group %q", b.Platform.Spec.Admins.Group)
	group, created, err := ad.EnsureGroup(ctx, b.Platform.Spec.Admins.Group)
	if err != nil {
		return err
	}
	if created {
		b.logf("    created %s", group)
	}
	me, err := ad.SignedInUser(ctx)
	if err != nil {
		return err
	}
	if err := ad.EnsureMember(ctx, group, me); err != nil {
		return err
	}
	b.IDs.Set(group, "admins", "groupObjectId")

	b.logf("==> CI identity (GitHub Actions in %s)", b.repo())
	ciApp, err := ad.EnsureApp(ctx, entra.App{DisplayName: name + "-ci"})
	if err != nil {
		return err
	}
	ciSP, err := ad.EnsureServicePrincipal(ctx, ciApp)
	if err != nil {
		return err
	}
	prefix, err := gh.SubjectPrefix(ctx, b.repo())
	if err != nil {
		return err
	}
	for _, env := range b.Platform.Spec.Environments {
		// Publishing jobs run in the GitHub environment named after the platform environment.
		if err := ad.EnsureFederatedCredential(ctx, ciApp, "github-env-"+env, entra.GitHubIssuer, prefix+":environment:"+env); err != nil {
			return err
		}
		if err := gh.EnsureEnvironment(ctx, b.repo(), env); err != nil {
			return err
		}
	}
	b.IDs.Set(ciApp, "ci", "clientId")
	b.IDs.Set(ciSP, "ci", "principalId")

	b.logf("==> Repository variables")
	vars := map[string]any{
		"BLUEPAVE_TENANT_ID":       b.IDs.Get("azure", "tenantId"),
		"BLUEPAVE_SUBSCRIPTION_ID": b.IDs.Get("azure", "subscriptionId"),
		"BLUEPAVE_CLIENT_ID":       ciApp,
	}
	for _, k := range []string{"BLUEPAVE_TENANT_ID", "BLUEPAVE_SUBSCRIPTION_ID", "BLUEPAVE_CLIENT_ID"} {
		if err := gh.SetVariable(ctx, b.repo(), k, fmt.Sprint(vars[k])); err != nil {
			return err
		}
	}
	return nil
}

// Identities runs the step after infra, for every environment.
func (b Bootstrap) Identities(ctx context.Context) error {
	for _, env := range b.Platform.Spec.Environments {
		if err := b.environment(ctx, env); err != nil {
			return fmt.Errorf("environment %s: %w", env, err)
		}
	}
	if len(b.Platform.Spec.Environments) > 0 {
		// The golden path publishes to the first environment's registry; Kargo promotes from there.
		env := b.Platform.Spec.Environments[0]
		if reg, _ := b.IDs.Get("environments", env, "registry", "containerRegistryLoginServer").(string); reg != "" {
			gh := github.Client{Runner: b.Runner}
			if err := gh.SetVariable(ctx, b.repo(), "BLUEPAVE_REGISTRY", reg); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b Bootstrap) environment(ctx context.Context, env string) error {
	ad := entra.Client{Runner: b.Runner}
	name := b.Platform.Metadata.Name
	domain := env + "." + b.Platform.Spec.DNS.Domain
	issuer, _ := b.IDs.Get("environments", env, "aks", "oidcIssuerUrl").(string)
	if issuer == "" {
		return fmt.Errorf("no cluster OIDC issuer recorded (aks.oidcIssuerUrl): run `bluepave up -step infra` first")
	}
	vaultName, _ := b.IDs.Get("environments", env, "keyvault", "keyVaultName").(string)
	kv := keyvault.Client{Runner: b.Runner, Vault: vaultName}

	// A sign-in app trusted by one service account: no client secret exists.
	federated := func(module, key, purpose, namespace, sa string, redirects []string) error {
		appID, err := ad.EnsureApp(ctx, entra.App{DisplayName: fmt.Sprintf("%s-%s-%s", name, purpose, env), RedirectURIs: redirects, GroupClaims: true})
		if err != nil {
			return err
		}
		if _, err := ad.EnsureServicePrincipal(ctx, appID); err != nil {
			return err
		}
		if err := ad.EnsureFederatedCredential(ctx, appID, sa, issuer, "system:serviceaccount:"+namespace+":"+sa); err != nil {
			return err
		}
		b.IDs.Set(appID, "environments", env, module, key)
		return nil
	}

	if b.enabled("gitops-argocd") {
		b.logf("==> %s: Argo CD sign-in (Entra app, Workload Identity)", env)
		if err := federated("gitops-argocd", "clientId", "argocd", "argocd", "argocd-server",
			[]string{"https://argocd." + domain + "/auth/callback", "https://localhost:8080/auth/callback"}); err != nil {
			return err
		}
	}
	if b.enabled("observability") {
		b.logf("==> %s: Grafana sign-in (Entra app, Workload Identity)", env)
		if err := federated("observability", "appClientId", "grafana", "observability", "grafana",
			[]string{"https://grafana." + domain + "/login/azuread"}); err != nil {
			return err
		}
	}
	if b.enabled("portal") && env == b.portalEnvironment() {
		b.logf("==> %s: portal sign-in (Entra app, client secret in Key Vault)", env)
		appID, err := ad.EnsureApp(ctx, entra.App{DisplayName: fmt.Sprintf("%s-portal-%s", name, env),
			RedirectURIs: []string{"https://portal." + domain + "/api/auth/microsoft/handler/frame"}})
		if err != nil {
			return err
		}
		if _, err := ad.EnsureServicePrincipal(ctx, appID); err != nil {
			return err
		}
		if err := b.ensureSecret(ctx, kv, "portal-entra-client-secret", func() (string, error) {
			return ad.NewClientSecret(ctx, appID, "bluepave portal")
		}); err != nil {
			return err
		}
		b.IDs.Set(appID, "environments", env, "portal", "appClientId")
	}
	if b.enabled("delivery-kargo") {
		b.logf("==> %s: Kargo admin credentials (Key Vault)", env)
		if err := b.kargoAdmin(ctx, kv); err != nil {
			return err
		}
	}
	return nil
}

func (b Bootstrap) portalEnvironment() string {
	if o, ok := b.Platform.Spec.Modules["portal"]; ok {
		if e, ok := o.Settings["environment"].(string); ok && e != "" {
			return e
		}
	}
	return "dev"
}

// ensureSecret stores a secret made by create, unless Key Vault already has it.
func (b Bootstrap) ensureSecret(ctx context.Context, kv keyvault.Client, name string, create func() (string, error)) error {
	if kv.Vault == "" {
		return fmt.Errorf("no Key Vault recorded (keyvault.keyVaultName): run `bluepave up -step infra` first")
	}
	has, err := kv.Has(ctx, name)
	if err != nil {
		return forbiddenHint(err)
	}
	if has {
		return nil
	}
	value, err := create()
	if err != nil {
		return err
	}
	return forbiddenHint(kv.Set(ctx, name, value))
}

// kargoAdmin creates Kargo's admin password (kept only in Key Vault), its bcrypt hash and the
// token-signing key, all three together so they always match.
func (b Bootstrap) kargoAdmin(ctx context.Context, kv keyvault.Client) error {
	names := []string{"kargo-admin-password", "kargo-admin-password-hash", "kargo-token-signing-key"}
	missing := false
	for _, n := range names {
		has, err := kv.Has(ctx, n)
		if err != nil {
			return forbiddenHint(err)
		}
		missing = missing || !has
	}
	if !missing {
		return nil
	}
	password, err := randomString(24)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return err
	}
	key, err := randomString(32)
	if err != nil {
		return err
	}
	for i, v := range []string{password, string(hash), key} {
		if err := kv.Set(ctx, names[i], v); err != nil {
			return forbiddenHint(err)
		}
	}
	return nil
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// forbiddenHint explains the usual cause of a Key Vault 403 on a first run: the admins group's
// Key Vault Administrator role reaches you only once your sign-in includes the new membership.
func forbiddenHint(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "forbidden") {
		return fmt.Errorf("%w\nKey Vault refused access. If you were just added to the admins group, sign in again (`az login`) and run `bluepave up -step identities`", err)
	}
	return err
}
