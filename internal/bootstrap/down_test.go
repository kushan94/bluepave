package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

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
	if err := b.waitStackGone(context.Background(), "bp-acme-dns"); err != nil {
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
