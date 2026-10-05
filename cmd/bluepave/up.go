package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/kushan94/bluepave/internal/bootstrap"
	"github.com/kushan94/bluepave/internal/deploy"
	"github.com/kushan94/bluepave/internal/discovered"
	"github.com/kushan94/bluepave/internal/githubapp"
	cmdrun "github.com/kushan94/bluepave/internal/run"
)

// runner is what commands use to reach Azure; tests replace it.
var runner cmdrun.Runner = cmdrun.Exec{Log: os.Stderr}

// selectStacks narrows the plan to one environment and/or one module.
func selectStacks(stacks []deploy.Stack, env, module string) []deploy.Stack {
	var out []deploy.Stack
	for _, s := range stacks {
		if env != "" && s.Env != "" && s.Env != env {
			continue
		}
		if module != "" && s.Module.Metadata.Name != module {
			continue
		}
		out = append(out, s)
	}
	return out
}

func planCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "bluepave.yaml", "platform configuration")
	root := fs.String("root", ".", "bluepave repository root")
	env := fs.String("env", "", "only this environment (global modules are always included)")
	module := fs.String("module", "", "only this module")
	whatIf := fs.Bool("what-if", false, "preview each stack's changes in Azure (needs az, signed in)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pl, err := load(*file, *root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	stacks := selectStacks(deploy.Plan(pl.config, pl.ordered), *env, *module)
	fmt.Fprintf(stdout, "%d deployment stacks, in order:\n", len(stacks))
	for i, s := range stacks {
		fmt.Fprintf(stdout, "  %2d. %-32s %s\n", i+1, s.Label(), s.Name)
	}
	if !*whatIf {
		return 0
	}
	ids, err := discovered.Load(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	eng := deploy.Engine{Runner: runner, Location: pl.config.Spec.Azure.Region}
	ctx := context.Background()
	status := 0
	for _, s := range stacks {
		fmt.Fprintf(stdout, "\n==> what-if %s\n", s.Label())
		out, err := eng.WhatIf(ctx, s, ids)
		if err != nil {
			// A later stack often can't be previewed before an earlier one exists; keep going.
			fmt.Fprintln(stderr, err)
			status = 1
			continue
		}
		stdout.Write(out)
	}
	return status
}

// steps of `bluepave up`, in order.
var steps = []string{"accounts", "infra", "identities", "github-app"}

// appFlow is how the github-app step reaches the user and GitHub; tests replace it.
var appFlow = bootstrap.AppFlow{
	OpenURL: func(url string) {
		fmt.Fprintf(os.Stderr, "    open: %s\n", url)
		openBrowser(url)
	},
	HTTP:    &http.Client{Timeout: 30 * time.Second},
	APIBase: "https://api.github.com",
	Poll:    5 * time.Second,
	Timeout: 15 * time.Minute,
	NewCode: func(ctx context.Context, f githubapp.Flow) (string, error) { return f.Code(ctx) },
}

// openBrowser tries the platform's opener; the URL is printed either way.
func openBrowser(url string) {
	for _, opener := range []string{"open", "xdg-open", "wslview"} {
		if path, err := exec.LookPath(opener); err == nil {
			_ = exec.Command(path, url).Start()
			return
		}
	}
}

func upCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "bluepave.yaml", "platform configuration")
	root := fs.String("root", ".", "bluepave repository root")
	step := fs.String("step", "", "run one step: "+strings.Join(steps, ", ")+" (default: all, in order)")
	env := fs.String("env", "", "infra: only this environment (global modules are always included)")
	module := fs.String("module", "", "infra: only this module")
	yes := fs.Bool("yes", false, "don't ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	run := steps
	if *step != "" {
		if !slices.Contains(steps, *step) {
			fmt.Fprintf(stderr, "unknown step %q (available: %s)\n", *step, strings.Join(steps, ", "))
			return 2
		}
		run = []string{*step}
	}
	pl, err := load(*file, *root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx := context.Background()
	ids, err := discovered.Load(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := recordAccount(ctx, ids); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	stacks := selectStacks(deploy.Plan(pl.config, pl.ordered), *env, *module)
	fmt.Fprintf(stdout, "Platform %q on subscription %v (%s). Steps: %s.\n", pl.config.Metadata.Name,
		ids.Get("azure", "subscriptionId"), pl.config.Spec.Azure.Region, strings.Join(run, ", "))
	if slices.Contains(run, "infra") {
		fmt.Fprintf(stdout, "Deployment stacks (%d):", len(stacks))
		for _, s := range stacks {
			fmt.Fprintf(stdout, " %s", s.Label())
		}
		fmt.Fprintln(stdout)
	}
	if !*yes && !confirm(stdout, "Continue?") {
		fmt.Fprintln(stderr, "cancelled")
		return 1
	}
	var enabled []string
	for _, m := range pl.ordered {
		enabled = append(enabled, m.Metadata.Name)
	}
	boot := bootstrap.Bootstrap{Runner: runner, Platform: pl.config, Modules: enabled, IDs: ids, Log: stdout}
	for _, st := range run {
		var err error
		switch st {
		case "accounts":
			err = boot.Accounts(ctx)
		case "infra":
			err = deployStacks(ctx, stdout, stderr, pl, stacks, ids, *root)
		case "identities":
			err = boot.Identities(ctx)
		case "github-app":
			err = boot.GitHubApp(ctx, appFlow)
		}
		// Save after every step (infra also saves after every stack): a failure keeps the progress.
		if saveErr := ids.Save(*root); saveErr != nil && err == nil {
			err = saveErr
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			fmt.Fprintf(stderr, "stopped in step %s; fix it and run `bluepave up` again (finished work is a no-op)\n", st)
			return 1
		}
	}
	fmt.Fprintf(stdout, "\nDone. Commit %s: GitOps reads the new IDs from Git.\n", discovered.Path)
	if ns, ok := ids.Get("global", "dns", "nameServers").([]any); ok && len(ns) > 0 {
		fmt.Fprintf(stdout, "Delegate %s to these name servers at your registrar: %v\n", pl.config.Spec.DNS.Domain, ns)
	}
	return 0
}

func deployStacks(ctx context.Context, stdout, stderr io.Writer, pl *platform, stacks []deploy.Stack, ids discovered.IDs, root string) error {
	eng := deploy.Engine{Runner: runner, Location: pl.config.Spec.Azure.Region}
	for i, s := range stacks {
		fmt.Fprintf(stdout, "\n==> [%d/%d] %s (stack %s)\n", i+1, len(stacks), s.Label(), s.Name)
		outputs, err := eng.Deploy(ctx, s, ids)
		if saveErr := ids.Save(root); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return fmt.Errorf("%s: %w", s.Label(), err)
		}
		for _, k := range sortedOutputKeys(outputs) {
			fmt.Fprintf(stdout, "    %s = %s\n", k, short(outputs[k]))
		}
	}
	return nil
}

// recordAccount stores the signed-in tenant and subscription; a different subscription than the
// one already recorded is refused (the IDs in the file would belong to another platform).
func recordAccount(ctx context.Context, ids discovered.IDs) error {
	out, err := runner.Run(ctx, "az", "account", "show", "--output", "json")
	if err != nil {
		return fmt.Errorf("not signed in to Azure? run `az login`: %w", err)
	}
	var acct struct {
		ID       string `json:"id"`
		TenantID string `json:"tenantId"`
	}
	if err := json.Unmarshal(out, &acct); err != nil {
		return fmt.Errorf("az account show: %w", err)
	}
	if prev, ok := ids.Get("azure", "subscriptionId").(string); ok && prev != "" && prev != acct.ID {
		return fmt.Errorf("signed in to subscription %s, but %s records %s; switch with `az account set --subscription %s`", acct.ID, discovered.Path, prev, prev)
	}
	ids.Set(acct.ID, "azure", "subscriptionId")
	ids.Set(acct.TenantID, "azure", "tenantId")
	return nil
}

func confirm(w io.Writer, question string) bool {
	fmt.Fprintf(w, "%s [y/N] ", question)
	var answer string
	fmt.Fscanln(os.Stdin, &answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

func sortedOutputKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func short(v any) string {
	s := fmt.Sprint(v)
	if b, err := json.Marshal(v); err == nil && !strings.HasPrefix(string(b), "\"") {
		s = string(b)
	}
	if len(s) > 100 {
		s = s[:97] + "..."
	}
	return s
}
