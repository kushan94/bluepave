package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kushan94/bluepave/internal/githubapp"
	"github.com/kushan94/bluepave/internal/keyvault"
)

// GitHub App secrets in every environment's Key Vault (read by secrets-external's ExternalSecrets).
const (
	secretAppID          = "github-app-id"
	secretInstallationID = "github-app-installation-id"
	secretClientID       = "github-app-client-id"
	secretPrivateKey     = "github-app-private-key"
)

// AppFlow holds what the GitHub App step needs besides the Runner; tests replace it.
type AppFlow struct {
	OpenURL func(url string) // shows (and opens) a URL for the user
	HTTP    *http.Client     // GitHub API client (installations, with the App's token)
	APIBase string           // https://api.github.com
	Poll    time.Duration    // how often to look for the installation
	Timeout time.Duration    // how long the user has to create and install the App
	NewCode func(ctx context.Context, f githubapp.Flow) (string, error)
}

// GitHubApp creates the platform's GitHub App (once, with the user in the browser), waits for it
// to be installed on the owner's account, and puts its credentials in every environment's Key
// Vault. Re-running reuses the App and only fills in what's missing.
func (b Bootstrap) GitHubApp(ctx context.Context, flow AppFlow) error {
	envs := b.Platform.Spec.Environments
	vaults := make([]keyvault.Client, 0, len(envs))
	for _, env := range envs {
		name, _ := b.IDs.Get("environments", env, "keyvault", "keyVaultName").(string)
		if name == "" {
			return fmt.Errorf("environment %s has no Key Vault recorded: run `bluepave up -step infra` first", env)
		}
		vaults = append(vaults, keyvault.Client{Runner: b.Runner, Vault: name})
	}
	owner := b.Platform.Spec.GitHub.Owner

	// The App's credentials: from the first Key Vault that has them, or a new App.
	creds := map[string]string{}
	for _, kv := range vaults {
		if ok, err := kv.Has(ctx, secretPrivateKey); err != nil {
			return forbiddenHint(err)
		} else if ok {
			for _, n := range []string{secretAppID, secretClientID, secretPrivateKey, secretInstallationID} {
				if has, _ := kv.Has(ctx, n); has {
					v, err := kv.Get(ctx, n)
					if err != nil {
						return forbiddenHint(err)
					}
					creds[n] = v
				}
			}
			break
		}
	}
	if creds[secretPrivateKey] == "" {
		app, err := b.createApp(ctx, flow, owner)
		if err != nil {
			return err
		}
		creds[secretAppID] = fmt.Sprint(app.ID)
		creds[secretClientID] = app.ClientID
		creds[secretPrivateKey] = app.PrivateKey
		b.IDs.Set(app.Slug, "github", "appSlug")
		// Store right away: the conversion code can't be used twice.
		if err := storeAll(ctx, vaults, creds); err != nil {
			return err
		}
	}
	b.IDs.Set(creds[secretAppID], "github", "appId")
	b.IDs.Set(creds[secretClientID], "github", "clientId")

	if creds[secretInstallationID] == "" {
		id, err := b.waitForInstallation(ctx, flow, owner, creds)
		if err != nil {
			return err
		}
		creds[secretInstallationID] = fmt.Sprint(id)
	}
	b.IDs.Set(creds[secretInstallationID], "github", "installationId")
	return storeAll(ctx, vaults, creds)
}

func (b Bootstrap) createApp(ctx context.Context, flow AppFlow, owner string) (*githubapp.App, error) {
	kind, err := b.Runner.Run(ctx, "gh", "api", "users/"+owner, "--jq", ".type")
	if err != nil {
		return nil, err
	}
	name := b.Platform.Spec.GitHub.AppName
	if name == "" {
		name = owner + "-" + b.Platform.Metadata.Name
	}
	if len(name) > 34 { // GitHub's limit for App names
		name = name[:34]
	}
	b.logf("==> GitHub App %q: confirm it in the browser (GitHub only creates Apps interactively)", name)
	f := githubapp.Flow{
		Owner:        owner,
		Organization: strings.TrimSpace(string(kind)) == "Organization",
		AppName:      name,
		Homepage:     "https://github.com/" + owner + "/" + b.Platform.Spec.GitHub.PlatformRepo,
		Open:         flow.OpenURL,
	}
	ctx, cancel := context.WithTimeout(ctx, flow.Timeout)
	defer cancel()
	code, err := flow.NewCode(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("waiting for the GitHub App to be created: %w", err)
	}
	return githubapp.Convert(ctx, b.Runner, code)
}

func (b Bootstrap) waitForInstallation(ctx context.Context, flow AppFlow, owner string, creds map[string]string) (int64, error) {
	var appID int64
	fmt.Sscan(creds[secretAppID], &appID)
	slug, _ := b.IDs.Get("github", "appSlug").(string)
	b.logf("==> Install the App on %s (at least the platform repository and the app repositories)", owner)
	if slug != "" {
		flow.OpenURL("https://github.com/apps/" + slug + "/installations/new")
	}
	ctx, cancel := context.WithTimeout(ctx, flow.Timeout)
	defer cancel()
	for {
		jwt, err := githubapp.JWT(appID, creds[secretPrivateKey], time.Now())
		if err != nil {
			return 0, err
		}
		id, err := githubapp.InstallationID(ctx, flow.HTTP, flow.APIBase, jwt, owner)
		if err != nil {
			return 0, err
		}
		if id != 0 {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("the App isn't installed on %s yet; install it and run `bluepave up -step github-app` again", owner)
		case <-time.After(flow.Poll):
		}
	}
}

// storeAll writes the credentials to every Key Vault that lacks them.
func storeAll(ctx context.Context, vaults []keyvault.Client, creds map[string]string) error {
	for _, kv := range vaults {
		for _, n := range []string{secretAppID, secretClientID, secretPrivateKey, secretInstallationID} {
			if creds[n] == "" {
				continue
			}
			has, err := kv.Has(ctx, n)
			if err != nil {
				return forbiddenHint(err)
			}
			if !has {
				if err := kv.Set(ctx, n, creds[n]); err != nil {
					return forbiddenHint(err)
				}
			}
		}
	}
	return nil
}
