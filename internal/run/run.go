// Package run executes the native tools bluepave orchestrates (az, gh, helm, kubectl). Everything
// that touches Azure, GitHub or a cluster goes through a Runner, so tests can record the commands
// instead of running them.
package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Runner runs one command and returns its standard output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	// RunIn is Run with standard input: how secrets reach a tool (kubectl apply -f -), never as
	// arguments.
	RunIn(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

// Exec runs commands for real. Their standard error goes to Log (progress, warnings), and is
// included in the error when the command fails.
type Exec struct {
	Log io.Writer
}

// Run implements Runner.
func (e Exec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return e.RunIn(ctx, nil, name, args...)
}

// RunIn implements Runner.
func (e Exec) RunIn(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if e.Log != nil {
		cmd.Stderr = io.MultiWriter(&stderr, e.Log)
	}
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Call is one recorded command.
type Call struct {
	Name  string
	Args  []string
	Stdin []byte
}

// String renders the call as a shell-like line, for tests and dry runs.
func (c Call) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// Recorder records commands instead of running them, answering each with the first matching
// canned response (by prefix of the command line), or empty output.
type Recorder struct {
	Calls     []Call
	Responses map[string]string
}

// Run implements Runner.
func (r *Recorder) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.RunIn(ctx, nil, name, args...)
}

// RunIn implements Runner.
func (r *Recorder) RunIn(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	c := Call{Name: name, Args: args, Stdin: stdin}
	r.Calls = append(r.Calls, c)
	line := c.String()
	longest := ""
	for prefix := range r.Responses {
		if strings.HasPrefix(line, prefix) && len(prefix) > len(longest) {
			longest = prefix
		}
	}
	if longest == "" {
		return nil, nil
	}
	return []byte(r.Responses[longest]), nil
}
