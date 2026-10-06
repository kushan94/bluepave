package bootstrap

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kushan94/bluepave/internal/config"
	"github.com/kushan94/bluepave/internal/deploy"
	"github.com/kushan94/bluepave/internal/modules"
	"github.com/kushan94/bluepave/internal/run"
)

// sequence answers each matching command with the next response, then the last one forever.
type sequence struct {
	run.Recorder
	prefix    string
	responses []string
}

func (s *sequence) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if strings.HasPrefix(line, s.prefix) {
		s.Calls = append(s.Calls, run.Call{Name: name, Args: args})
		r := s.responses[0]
		if len(s.responses) > 1 {
			s.responses = s.responses[1:]
		}
		return []byte(r), nil
	}
	return s.Recorder.Run(ctx, name, args...)
}

// A stack an earlier, interrupted `down` is still deleting is waited for, not deleted again
// (Azure refuses: DeploymentStackInNonTerminalState).
func TestWaitStackGone(t *testing.T) {
	r := &sequence{prefix: "az stack sub list", responses: []string{"deletingResources", "deletingResources", ""}}
	b := Bootstrap{Runner: r, PollInterval: time.Millisecond}
	if err := b.waitStackGone(context.Background(), "bp-acme-dns", "deletingResources"); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Calls {
		if strings.Contains(c.String(), "stack sub delete") {
			t.Errorf("deleted a stack that was already being deleted: %s", c)
		}
	}
	if got := len(r.Calls); got != 3 {
		t.Errorf("polled %d times, want 3", got)
	}
}

// The real-account run: self-service's stack can't go while data-postgres's server still holds
// what it owns; once data-postgres is gone, the second try succeeds.
func TestDeleteStacksRetriesFailed(t *testing.T) {
	gone := map[string]bool{}
	attempts := map[string]int{}
	deleting := map[string]int{}
	r := &run.Recorder{Handlers: []func(string) (string, bool){func(line string) (string, bool) {
		name := ""
		if i := strings.Index(line, "[?name=='"); i >= 0 {
			name = line[i+len("[?name=='"):]
			name = name[:strings.Index(name, "'")]
		}
		switch {
		case strings.HasPrefix(line, "az rest --method delete"):
			n := line[strings.Index(line, "deploymentStacks/")+len("deploymentStacks/"):]
			n = n[:strings.Index(n, "?")]
			attempts[n]++
			deleting[n] = 2 // two polls in deletingResources
			return "", true
		case strings.HasPrefix(line, "az stack sub list"):
			if gone[name] {
				return "", true
			}
			if deleting[name] > 0 {
				deleting[name]--
				return "deletingResources", true
			}
			if attempts[name] == 0 {
				return "succeeded", true
			}
			// self-service's first delete fails (data-postgres still holds what it owns) and the
			// stack stays failed, as in Azure, until it's deleted again.
			if name == "bp-acme-dev-self-service" && attempts[name] == 1 {
				return "failed", true
			}
			gone[name] = true
			return "", true
		case strings.HasPrefix(line, "az stack sub show"):
			return "The request to delete the resource 'administrators/x' failed.", true
		}
		return "", false
	}}}
	stacks := []deploy.Stack{
		{Module: &modules.Module{}, Env: "dev", Name: "bp-acme-dev-data-postgres"},
		{Module: &modules.Module{}, Env: "dev", Name: "bp-acme-dev-self-service"},
	}
	b := Bootstrap{Runner: r, PollInterval: time.Millisecond, Log: io.Discard}
	if problems := b.deleteStacks(context.Background(), stacks); len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	if attempts["bp-acme-dev-self-service"] != 2 || attempts["bp-acme-dev-data-postgres"] != 1 {
		t.Errorf("attempts = %v, want self-service twice, data-postgres once", attempts)
	}
	for _, c := range r.Calls {
		if strings.HasPrefix(c.String(), "az stack sub delete") {
			t.Errorf("blocking delete: %s", c)
		}
	}
}

// A deletion that keeps failing is reported with what blocked it.
func TestDeleteStacksReportsFailure(t *testing.T) {
	r := &run.Recorder{Handlers: []func(string) (string, bool){func(line string) (string, bool) {
		switch {
		case strings.HasPrefix(line, "az rest --method delete"):
			return "", true
		case strings.HasPrefix(line, "az stack sub list"):
			return "failed", true
		case strings.HasPrefix(line, "az stack sub show"):
			return "The request to delete the resource 'administrators/x' failed.", true
		}
		return "", false
	}}}
	b := Bootstrap{Runner: r, PollInterval: time.Millisecond, Log: io.Discard}
	problems := b.deleteStacks(context.Background(), []deploy.Stack{{Module: &modules.Module{}, Env: "dev", Name: "bp-acme-dev-self-service"}})
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "administrators/x") {
		t.Fatalf("problems = %v", problems)
	}
}

// failing fails every command that starts with prefix with err.
type failing struct {
	run.Recorder
	prefix string
	err    error
}

func (f *failing) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, _ := f.Recorder.Run(ctx, name, args...)
	if strings.HasPrefix(strings.TrimSpace(name+" "+strings.Join(args, " ")), f.prefix) {
		return nil, f.err
	}
	return out, nil
}

// When the platform repository is already deleted, its variables are gone with it.
func TestDeleteVariablesRepoGone(t *testing.T) {
	p := &config.Platform{}
	p.Spec.GitHub.Owner, p.Spec.GitHub.PlatformRepo = "acme", "platform"
	for _, tc := range []struct {
		err      string
		problems int
	}{
		{"failed to get variables: HTTP 404: Not Found (https://api.github.com/repos/acme/platform/actions/variables)", 0},
		{"failed to get variables: HTTP 403: Forbidden", 1},
	} {
		r := &failing{prefix: "gh variable list", err: errors.New(tc.err)}
		b := Bootstrap{Runner: r, Platform: p, Log: io.Discard}
		if problems := b.deleteVariables(context.Background()); len(problems) != tc.problems {
			t.Errorf("%s: problems = %v, want %d", tc.err, problems, tc.problems)
		}
		if len(r.Calls) != 1 {
			t.Errorf("%s: calls = %v, want only the list", tc.err, r.Calls)
		}
	}
}
