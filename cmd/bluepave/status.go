package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kushan94/bluepave/internal/bootstrap"
	"github.com/kushan94/bluepave/internal/deploy"
	"github.com/kushan94/bluepave/internal/discovered"
	"github.com/kushan94/bluepave/internal/modules"
)

func statusCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "bluepave.yaml", "platform configuration")
	root := fs.String("root", ".", "bluepave repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pl, err := load(*file, *root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ids, err := discovered.Load(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx := context.Background()
	healthy := true

	fmt.Fprintln(stdout, "Deployment stacks:")
	for _, s := range deploy.Plan(pl.config, pl.ordered) {
		out, err := runner.Run(ctx, "az", "stack", "sub", "show", "--name", s.Name, "--query", "provisioningState", "--output", "tsv")
		state := strings.TrimSpace(string(out))
		if err != nil || state == "" {
			state = "not deployed"
		}
		healthy = healthy && strings.EqualFold(state, "succeeded")
		fmt.Fprintf(stdout, "  %-32s %s\n", s.Label(), state)
	}

	for _, env := range pl.config.Spec.Environments {
		cluster, _ := ids.Get("environments", env, "aks", "clusterName").(string)
		rg, _ := ids.Get("environments", env, "aks", "resourceGroupName").(string)
		if cluster == "" || rg == "" {
			continue
		}
		fmt.Fprintf(stdout, "\nArgo CD applications (%s):\n", env)
		apps, err := argoApplications(ctx, rg, cluster)
		if err != nil {
			fmt.Fprintf(stdout, "  unavailable: %v\n", firstLine(err))
			healthy = false
			continue
		}
		for _, a := range apps {
			healthy = healthy && a.sync == "Synced" && a.health == "Healthy"
			fmt.Fprintf(stdout, "  %-32s %-10s %s\n", a.name, a.sync, a.health)
		}
		if enabledModule(pl, "edge-gateway") {
			fmt.Fprintf(stdout, "\nURLs (%s):\n", env)
			domain := env + "." + pl.config.Spec.DNS.Domain
			for _, ui := range []struct{ module, host string }{{"portal", "portal"}, {"gitops-argocd", "argocd"}, {"observability", "grafana"}} {
				if enabledModule(pl, ui.module) {
					fmt.Fprintf(stdout, "  https://%s.%s\n", ui.host, domain)
				}
			}
		}
	}
	if ns, ok := ids.Get("global", "dns", "nameServers").([]any); ok && len(ns) > 0 {
		fmt.Fprintf(stdout, "\n%s must be delegated to: %v\n", pl.config.Spec.DNS.Domain, ns)
	}
	if !healthy {
		return 1
	}
	return 0
}

type appStatus struct{ name, sync, health string }

func argoApplications(ctx context.Context, rg, cluster string) ([]appStatus, error) {
	tmp, err := os.MkdirTemp("", "bluepave-status-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	kubeconfig := filepath.Join(tmp, "kubeconfig")
	if _, err := runner.Run(ctx, "az", "aks", "get-credentials", "--resource-group", rg, "--name", cluster,
		"--file", kubeconfig, "--overwrite-existing", "--output", "none"); err != nil {
		return nil, err
	}
	if _, err := runner.Run(ctx, "kubelogin", "convert-kubeconfig", "--login", "azurecli", "--kubeconfig", kubeconfig); err != nil {
		return nil, err
	}
	out, err := runner.Run(ctx, "kubectl", "--kubeconfig", kubeconfig, "get", "applications.argoproj.io", "--namespace", "argocd", "--output", "json")
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Sync struct {
					Status string `json:"status"`
				} `json:"sync"`
				Health struct {
					Status string `json:"status"`
				} `json:"health"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	var apps []appStatus
	for _, it := range list.Items {
		apps = append(apps, appStatus{it.Metadata.Name, it.Status.Sync.Status, it.Status.Health.Status})
	}
	slices.SortFunc(apps, func(a, b appStatus) int { return strings.Compare(a.name, b.name) })
	return apps, nil
}

func downCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "bluepave.yaml", "platform configuration")
	root := fs.String("root", ".", "bluepave repository root")
	yes := fs.Bool("yes", false, "don't ask (for automated end-to-end tests)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pl, err := load(*file, *root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ids, err := discovered.Load(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx := context.Background()
	if err := recordAccount(ctx, ids); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	stacks := deploy.Plan(pl.config, pl.ordered)
	name := pl.config.Metadata.Name
	purge := !pl.profile.Spec.KeyVault.PurgeProtection
	fmt.Fprintf(stdout, "This deletes platform %q from subscription %v:\n", name, ids.Get("azure", "subscriptionId"))
	fmt.Fprintln(stdout, "  - the platform's GitHub App and its installations")
	fmt.Fprintf(stdout, "  - the Entra apps up created (%s-ci, -argocd-*, -grafana-*, -portal-*)\n", name)
	fmt.Fprintf(stdout, "  - %d deployment stacks and every resource in them: clusters, databases, registries, Key Vaults, DNS zones\n", len(stacks))
	if purge {
		fmt.Fprintln(stdout, "  - the soft-deleted Key Vaults (purged: the profile has no purge protection)")
	}
	fmt.Fprintln(stdout, "  - the BLUEPAVE_* repository variables")
	fmt.Fprintf(stdout, "It keeps the admins group %q and the GitHub environments.\n", pl.config.Spec.Admins.Group)
	if !*yes {
		fmt.Fprintf(stdout, "Type the platform's name (%s) to continue: ", name)
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(answer) != name {
			fmt.Fprintln(stderr, "cancelled")
			return 1
		}
	}
	boot := bootstrap.Bootstrap{Runner: runner, Platform: pl.config, IDs: ids, Log: stdout}
	problems := boot.Down(ctx, stacks, appFlow, purge)
	if len(problems) > 0 {
		fmt.Fprintf(stderr, "\n%d problems; fix them and run `bluepave down` again:\n", len(problems))
		for _, p := range problems {
			fmt.Fprintf(stderr, "  - %v\n", firstLine(p))
		}
		if err := ids.Save(*root); err != nil {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	bootstrap.Reset(ids)
	if err := ids.Save(*root); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "\nDone. %s is reset; commit it. Remove the DNS delegation of %s at your registrar.\n", discovered.Path, pl.config.Spec.DNS.Domain)
	return 0
}

func firstLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func enabledModule(pl *platform, name string) bool {
	return slices.ContainsFunc(pl.ordered, func(m *modules.Module) bool { return m.Metadata.Name == name })
}
