// Package entra creates the Microsoft Entra ID objects a platform needs (the admins group, app
// registrations, federated credentials) with the Azure CLI. Every function converges: running it
// again changes nothing that's already right.
package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kushan94/bluepave/internal/run"
)

// GitHubIssuer is the issuer of GitHub Actions OIDC tokens.
const GitHubIssuer = "https://token.actions.githubusercontent.com"

// Microsoft Graph's well-known, public identifiers (the same in every tenant), not credentials.
const (
	graphAppID    = "00000003-0000-0000-c000-000000000000"
	graphUserRead = "e1fe6dd8-ba31-4d61-89e7-88639da4683d" // gitleaks:allow (public permission ID: User.Read)
)

// Client wraps the Azure CLI's `az ad` commands.
type Client struct {
	Runner run.Runner
}

func (c Client) tsv(ctx context.Context, args ...string) (string, error) {
	out, err := c.Runner.Run(ctx, "az", append(args, "--output", "tsv")...)
	return strings.TrimSpace(string(out)), err
}

// SignedInUser returns the object ID of the user running the CLI.
func (c Client) SignedInUser(ctx context.Context) (string, error) {
	return c.tsv(ctx, "ad", "signed-in-user", "show", "--query", "id")
}

// EnsureGroup returns the security group with this display name, creating it if missing.
func (c Client) EnsureGroup(ctx context.Context, name string) (id string, created bool, err error) {
	id, err = c.tsv(ctx, "ad", "group", "list", "--display-name", name, "--query", "[0].id")
	if err != nil || id != "" {
		return id, false, err
	}
	id, err = c.tsv(ctx, "ad", "group", "create", "--display-name", name, "--mail-nickname", nickname(name), "--query", "id")
	return id, err == nil, err
}

// EnsureMember adds a member to a group unless it's already in it.
func (c Client) EnsureMember(ctx context.Context, groupID, memberID string) error {
	in, err := c.tsv(ctx, "ad", "group", "member", "check", "--group", groupID, "--member-id", memberID, "--query", "value")
	if err != nil {
		return err
	}
	if in == "true" {
		return nil
	}
	_, err = c.Runner.Run(ctx, "az", "ad", "group", "member", "add", "--group", groupID, "--member-id", memberID)
	return err
}

// App is an app registration to converge on.
type App struct {
	DisplayName  string
	RedirectURIs []string // web redirect URIs; empty for a workload-only app (CI)
	GroupClaims  bool     // put the user's security group IDs in tokens
}

// EnsureApp returns the app registration's application (client) ID, creating it if missing, and
// converges its redirect URIs and group claims. Sign-in apps get Microsoft Graph User.Read.
func (c Client) EnsureApp(ctx context.Context, app App) (appID string, err error) {
	appID, err = c.tsv(ctx, "ad", "app", "list", "--display-name", app.DisplayName, "--query", "[0].appId")
	if err != nil {
		return "", err
	}
	if appID == "" {
		appID, err = c.tsv(ctx, "ad", "app", "create", "--display-name", app.DisplayName, "--sign-in-audience", "AzureADMyOrg", "--query", "appId")
		if err != nil {
			return "", err
		}
		if len(app.RedirectURIs) > 0 {
			// Microsoft Graph User.Read (delegated), needed to sign users in.
			if _, err := c.Runner.Run(ctx, "az", "ad", "app", "permission", "add", "--id", appID,
				"--api", graphAppID, "--api-permissions", graphUserRead+"=Scope"); err != nil {
				return "", err
			}
		}
	}
	update := []string{"ad", "app", "update", "--id", appID}
	if len(app.RedirectURIs) > 0 {
		update = append(append(update, "--web-redirect-uris"), app.RedirectURIs...)
	}
	if app.GroupClaims {
		update = append(update, "--set", "groupMembershipClaims=SecurityGroup")
	}
	if len(update) > 5 {
		if _, err := c.Runner.Run(ctx, "az", update...); err != nil {
			return "", err
		}
	}
	return appID, nil
}

// EnsureServicePrincipal returns the object ID of the app's service principal, creating it if
// missing. Role assignments and group memberships use this ID.
func (c Client) EnsureServicePrincipal(ctx context.Context, appID string) (string, error) {
	id, err := c.tsv(ctx, "ad", "sp", "list", "--filter", fmt.Sprintf("appId eq '%s'", appID), "--query", "[0].id")
	if err != nil || id != "" {
		return id, err
	}
	return c.tsv(ctx, "ad", "sp", "create", "--id", appID, "--query", "id")
}

// EnsureFederatedCredential makes the app trust tokens for one subject from one issuer (a GitHub
// environment, or a Kubernetes service account through the cluster's OIDC issuer). A credential
// with the same name but another issuer or subject (a re-created cluster) is replaced.
func (c Client) EnsureFederatedCredential(ctx context.Context, appID, name, issuer, subject string) error {
	out, err := c.Runner.Run(ctx, "az", "ad", "app", "federated-credential", "list", "--id", appID,
		"--query", fmt.Sprintf("[?name=='%s'] | [0]", name), "--output", "json")
	if err != nil {
		return err
	}
	var current struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
	}
	if s := strings.TrimSpace(string(out)); s != "" && s != "null" {
		if err := json.Unmarshal(out, &current); err != nil {
			return fmt.Errorf("federated credential %s: %w", name, err)
		}
		if current.Issuer == issuer && current.Subject == subject {
			return nil
		}
		if _, err := c.Runner.Run(ctx, "az", "ad", "app", "federated-credential", "delete", "--id", appID, "--federated-credential-id", name); err != nil {
			return err
		}
	}
	params, _ := json.Marshal(map[string]any{
		"name": name, "issuer": issuer, "subject": subject, "audiences": []string{"api://AzureADTokenExchange"},
	})
	_, err = c.Runner.Run(ctx, "az", "ad", "app", "federated-credential", "create", "--id", appID, "--parameters", string(params))
	return err
}

// NewClientSecret adds a client secret to the app and returns it. The caller stores it (Key Vault)
// and must never print it.
func (c Client) NewClientSecret(ctx context.Context, appID, displayName string) (string, error) {
	return c.tsv(ctx, "ad", "app", "credential", "reset", "--id", appID, "--append",
		"--display-name", displayName, "--years", "1", "--query", "password")
}

// nickname turns a display name into a valid mail nickname (letters, digits, - and _).
func nickname(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
