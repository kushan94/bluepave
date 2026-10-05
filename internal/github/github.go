// Package github sets up the platform's GitHub repositories with the GitHub CLI: environments,
// repository variables, and the OIDC subject prefix Azure has to trust.
package github

import (
	"context"
	"strings"

	"github.com/kushan94/bluepave/internal/run"
)

// Client wraps `gh`.
type Client struct {
	Runner run.Runner
}

// SubjectPrefix returns the prefix of the OIDC token subjects for a repository ("owner/name").
// Repositories with immutable subject claims use "repo:<owner>@<owner-id>/<repo>@<repo-id>", so a
// renamed or re-created repository with the same name can't sign in to Azure; others use
// "repo:<owner>/<repo>".
func (c Client) SubjectPrefix(ctx context.Context, repo string) (string, error) {
	out, err := c.Runner.Run(ctx, "gh", "api", "repos/"+repo+"/actions/oidc/customization/sub", "--jq", ".sub_claim_prefix // empty")
	if err != nil {
		return "", err
	}
	if p := strings.TrimSpace(string(out)); p != "" {
		return p, nil
	}
	return "repo:" + repo, nil
}

// EnsureEnvironment creates or updates a GitHub environment that only the main branch may use:
// Azure trusts "environment:<env>" whatever the branch, so the branch policy is what keeps a
// branch from publishing.
func (c Client) EnsureEnvironment(ctx context.Context, repo, env string) error {
	if _, err := c.Runner.Run(ctx, "gh", "api", "--method", "PUT", "repos/"+repo+"/environments/"+env,
		"-F", "deployment_branch_policy[protected_branches]=false",
		"-F", "deployment_branch_policy[custom_branch_policies]=true", "--silent"); err != nil {
		return err
	}
	out, err := c.Runner.Run(ctx, "gh", "api", "repos/"+repo+"/environments/"+env+"/deployment-branch-policies",
		"--jq", `[.branch_policies[] | select(.name == "main")] | length`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "0" && strings.TrimSpace(string(out)) != "" {
		return nil
	}
	_, err = c.Runner.Run(ctx, "gh", "api", "--method", "POST", "repos/"+repo+"/environments/"+env+"/deployment-branch-policies",
		"-f", "name=main", "-f", "type=branch", "--silent")
	return err
}

// SetVariable sets a repository variable (not a secret: IDs only).
func (c Client) SetVariable(ctx context.Context, repo, name, value string) error {
	_, err := c.Runner.Run(ctx, "gh", "variable", "set", name, "--repo", repo, "--body", value)
	return err
}
