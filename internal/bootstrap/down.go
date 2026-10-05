package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kushan94/bluepave/internal/deploy"
	"github.com/kushan94/bluepave/internal/discovered"
	"github.com/kushan94/bluepave/internal/githubapp"
	"github.com/kushan94/bluepave/internal/keyvault"
)

// Down removes what `up` created, in an order that works: the GitHub App (its key is in Key
// Vault), the Entra apps, the deployment stacks in reverse (deleting their resources), soft-deleted
// Key Vaults where purging is allowed, and the repository variables. The admins group stays: it
// may have existed before the platform. It returns the problems it couldn't fix, and keeps going.
func (b Bootstrap) Down(ctx context.Context, stacks []deploy.Stack, flow AppFlow, purgeVaults bool) []error {
	var problems []error
	try := func(what string, err error) {
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", what, err))
			b.logf("    failed: %v", err)
		}
	}
	name := b.Platform.Metadata.Name
	envs := b.Platform.Spec.Environments

	b.logf("==> GitHub App")
	try("GitHub App", b.deleteApp(ctx, flow))

	b.logf("==> Entra apps")
	apps := []string{name + "-ci"}
	for _, env := range envs {
		for _, purpose := range []string{"argocd", "grafana", "portal"} {
			apps = append(apps, fmt.Sprintf("%s-%s-%s", name, purpose, env))
		}
	}
	for _, app := range apps {
		out, err := b.Runner.Run(ctx, "az", "ad", "app", "list", "--display-name", app, "--query", "[0].appId", "--output", "tsv")
		if err != nil {
			try(app, err)
			continue
		}
		if id := strings.TrimSpace(string(out)); id != "" {
			b.logf("    %s", app)
			_, err := b.Runner.Run(ctx, "az", "ad", "app", "delete", "--id", id)
			try(app, err)
		}
	}

	b.logf("==> Deployment stacks (newest first; their resources are deleted)")
	for i := len(stacks) - 1; i >= 0; i-- {
		s := stacks[i]
		if _, err := b.Runner.Run(ctx, "az", "stack", "sub", "show", "--name", s.Name, "--query", "id", "--output", "tsv"); err != nil {
			continue // not deployed
		}
		b.logf("    %s", s.Label())
		_, err := b.Runner.Run(ctx, "az", "stack", "sub", "delete", "--name", s.Name, "--action-on-unmanage", "deleteAll", "--yes")
		try("stack "+s.Name, err)
	}

	if purgeVaults {
		b.logf("==> Soft-deleted Key Vaults (purged, so the next `up` can reuse their names)")
		for _, env := range envs {
			vault, _ := b.IDs.Get("environments", env, "keyvault", "keyVaultName").(string)
			if vault == "" {
				continue
			}
			out, _ := b.Runner.Run(ctx, "az", "keyvault", "list-deleted", "--query", fmt.Sprintf("[?name=='%s'] | length(@)", vault), "--output", "tsv")
			if strings.TrimSpace(string(out)) == "1" {
				b.logf("    %s", vault)
				_, err := b.Runner.Run(ctx, "az", "keyvault", "purge", "--name", vault)
				try("purge "+vault, err)
			}
		}
	}

	b.logf("==> Repository variables")
	for _, v := range []string{"BLUEPAVE_TENANT_ID", "BLUEPAVE_SUBSCRIPTION_ID", "BLUEPAVE_CLIENT_ID", "BLUEPAVE_REGISTRY"} {
		// Missing variables are fine.
		_, _ = b.Runner.Run(ctx, "gh", "variable", "delete", v, "--repo", b.repo())
	}
	return problems
}

func (b Bootstrap) deleteApp(ctx context.Context, flow AppFlow) error {
	var kv keyvault.Client
	for _, env := range b.Platform.Spec.Environments {
		if vault, _ := b.IDs.Get("environments", env, "keyvault", "keyVaultName").(string); vault != "" {
			kv = keyvault.Client{Runner: b.Runner, Vault: vault}
			break
		}
	}
	if kv.Vault == "" {
		return nil
	}
	if has, err := kv.Has(ctx, secretPrivateKey); err != nil || !has {
		return forbiddenHint(err)
	}
	key, err := kv.Get(ctx, secretPrivateKey)
	if err != nil {
		return forbiddenHint(err)
	}
	idText, err := kv.Get(ctx, secretAppID)
	if err != nil {
		return forbiddenHint(err)
	}
	var id int64
	fmt.Sscan(idText, &id)
	jwt, err := githubapp.JWT(id, key, time.Now())
	if err != nil {
		return err
	}
	if err := githubapp.Delete(ctx, flow.HTTP, flow.APIBase, jwt); err != nil {
		slug, _ := b.IDs.Get("github", "appSlug").(string)
		return fmt.Errorf("%w; delete it at https://github.com/settings/apps/%s", err, slug)
	}
	return nil
}

// Reset empties the discovered IDs after `down`: they describe resources that no longer exist.
func Reset(ids discovered.IDs) {
	for k := range ids {
		delete(ids, k)
	}
}
