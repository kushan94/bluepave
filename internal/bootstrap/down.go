package bootstrap

import (
	"context"
	"fmt"
	"slices"
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

	problems = append(problems, b.deleteStacks(ctx, stacks)...)

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
	problems = append(problems, b.deleteVariables(ctx)...)
	return problems
}

// deleteVariables deletes the platform's repository variables. Only the ones that exist
// (BLUEPAVE_REGISTRY comes late in `up`), so gh prints no 404s; none when the repository is gone.
func (b Bootstrap) deleteVariables(ctx context.Context) []error {
	out, err := b.Runner.Run(ctx, "gh", "variable", "list", "--repo", b.repo(), "--json", "name", "--jq", ".[].name")
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			b.logf("    none: repository %s is gone", b.repo())
			return nil
		}
		b.logf("    failed: %v", err)
		return []error{fmt.Errorf("list repository variables: %w", err)}
	}
	var problems []error
	existing := strings.Fields(string(out))
	for _, v := range []string{"BLUEPAVE_TENANT_ID", "BLUEPAVE_SUBSCRIPTION_ID", "BLUEPAVE_CLIENT_ID", "BLUEPAVE_REGISTRY"} {
		if slices.Contains(existing, v) {
			if _, err := b.Runner.Run(ctx, "gh", "variable", "delete", v, "--repo", b.repo()); err != nil {
				b.logf("    failed: %v", err)
				problems = append(problems, fmt.Errorf("variable %s: %w", v, err))
			}
		}
	}
	return problems
}

// deleteStacks deletes the stacks newest first, then tries the ones that failed once more: a
// stack can fail because a later one still holds a dependency (e.g. self-service's PostgreSQL
// administrator owns databases on data-postgres's server, which goes with that stack).
func (b Bootstrap) deleteStacks(ctx context.Context, stacks []deploy.Stack) []error {
	var problems []error
	b.logf("==> Deployment stacks (newest first; their resources are deleted)")
	var failed []deploy.Stack
	for i := len(stacks) - 1; i >= 0; i-- {
		s := stacks[i]
		state, err := b.stackState(ctx, s.Name)
		if err != nil || state == "" {
			continue // not deployed
		}
		b.logf("    %s", s.Label())
		if err := b.deleteStack(ctx, s.Name, state); err != nil {
			b.logf("      failed, retried at the end: %v", err)
			failed = append(failed, s)
		}
	}
	if len(failed) > 0 {
		b.logf("==> Stacks that failed, again (what blocked them may be gone now)")
		for _, s := range failed {
			b.logf("    %s", s.Label())
			state, err := b.stackState(ctx, s.Name)
			if err != nil || state == "" {
				continue
			}
			if err := b.deleteStack(ctx, s.Name, state); err != nil {
				problems = append(problems, fmt.Errorf("stack %s: %w", s.Name, err))
			}
		}
	}
	return problems
}

// stackState is a deployment stack's provisioning state, or "" when it doesn't exist. A list
// query, not `show`: a stack that was never deployed is no result, not an error on the terminal.
func (b Bootstrap) stackState(ctx context.Context, name string) (string, error) {
	out, err := b.az(ctx, "stack", "sub", "list", "--query", fmt.Sprintf("[?name=='%s'].provisioningState", name), "--output", "tsv")
	return strings.TrimSpace(string(out)), err
}

// deleteStack deletes a stack and its resources, and waits until it's gone. It submits the delete
// (REST) and polls with short requests, so a dropped connection can't hang it (`az stack sub
// delete` holds one request open for the whole deletion, and has no --no-wait). A stack already being deleted (an
// earlier, interrupted `down`) is only waited for: Azure refuses a second delete.
func (b Bootstrap) deleteStack(ctx context.Context, name, state string) error {
	if !strings.EqualFold(state, "deletingResources") && !strings.EqualFold(state, "deleting") {
		// `az stack sub delete` has no --no-wait; the REST call returns once Azure accepts it.
		sub, _ := b.IDs.Get("azure", "subscriptionId").(string)
		url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.Resources/deploymentStacks/%s"+
			"?api-version=2024-03-01&unmanageAction.Resources=delete&unmanageAction.ResourceGroups=delete&unmanageAction.ManagementGroups=delete", sub, name)
		if _, err := b.az(ctx, "rest", "--method", "delete", "--url", url); err != nil {
			return err
		}
	}
	return b.waitStackGone(ctx, name, state)
}

// waitStackGone polls until the stack no longer exists (at most 90 minutes). A deletion that
// fails leaves the stack in "failed": that's reported with the resources that blocked it, once the
// stack has left the state it was in before (so an earlier failure isn't mistaken for this one).
func (b Bootstrap) waitStackGone(ctx context.Context, name, before string) error {
	moved := false
	for i := 0; i < 360; i++ {
		state, err := b.stackState(ctx, name)
		if err != nil {
			return err
		}
		if state == "" {
			return nil
		}
		if !strings.EqualFold(state, before) || i >= 6 {
			moved = true
		}
		if moved && strings.EqualFold(state, "failed") {
			out, _ := b.az(ctx, "stack", "sub", "show", "--name", name, "--query", "failedResources[].error.message", "--output", "tsv")
			return fmt.Errorf("deleting stack %s failed: %s", name, strings.Join(strings.Fields(string(out)), " "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.pollInterval()):
		}
	}
	return fmt.Errorf("stack %s is still being deleted after 90 minutes; run `bluepave down` again later", name)
}

// az runs one short az request with a timeout, retrying when it fails or times out (a dropped
// connection), up to 5 times.
func (b Bootstrap) az(ctx context.Context, args ...string) ([]byte, error) {
	var lastErr error
	for i := 0; i < 5; i++ {
		c, cancel := context.WithTimeout(ctx, 3*time.Minute)
		out, err := b.Runner.Run(c, "az", args...)
		cancel()
		if err == nil {
			return out, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
		if !networkError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// networkError reports whether an az failure looks like the network, not the request.
func networkError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection reset", "connection aborted", "timed out", "timeout",
		"signal: killed", "context deadline exceeded", "temporary failure", "eof", "tls handshake",
		"remote end closed", "connection refused", "502", "503", "504"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// pollInterval is how often waits poll Azure (PollInterval, or 30s).
func (b Bootstrap) pollInterval() time.Duration {
	if b.PollInterval > 0 {
		return b.PollInterval
	}
	return 30 * time.Second
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
