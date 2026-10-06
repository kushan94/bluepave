package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/kushan94/bluepave/internal/discovered"
	"github.com/kushan94/bluepave/internal/modules"
	"github.com/kushan94/bluepave/internal/preflight"
)

func preflightCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
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
	if err := recordAccount(ctx, ids); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := runPreflight(ctx, pl, ids, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// runPreflight checks that the subscription can run the profile in the region (VM sizes, zones,
// quota, PostgreSQL), printing what it found; an error means `up` would fail partway.
func runPreflight(ctx context.Context, pl *platform, ids discovered.IDs, w io.Writer) error {
	prof, err := preflight.LoadProfile(pl.root, pl.config.Spec.Profile)
	if err != nil {
		return err
	}
	// Quota is needed only for clusters that don't exist yet.
	clusters := 0
	for _, env := range pl.config.Spec.Environments {
		if name, _ := ids.Get("environments", env, "aks", "clusterName").(string); name == "" {
			clusters++
		}
	}
	enabled := func(name string) bool {
		return slices.ContainsFunc(pl.ordered, func(m *modules.Module) bool { return m.Metadata.Name == name })
	}
	postgres := enabled("data-postgres")
	// Each cluster: one public IP for egress, one for the Gateway's load balancer.
	publicIPs := 1
	if enabled("edge-gateway") {
		publicIPs++
	}
	sub, _ := ids.Get("azure", "subscriptionId").(string)
	region := pl.config.Spec.Azure.Region
	fmt.Fprintf(w, "==> Preflight: profile %s in %s\n", pl.config.Spec.Profile, region)
	res, err := preflight.Check(ctx, runner, preflight.Input{
		SubscriptionID: sub,
		Region:         region,
		Profile:        prof,
		Clusters:       clusters,
		Postgres:       postgres,
		PublicIPs:      publicIPs,
	})
	if err != nil {
		return err
	}
	for _, n := range res.Notes {
		fmt.Fprintf(w, "    ok: %s\n", n)
	}
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintf(w, "    PROBLEM: %s\n", p)
		}
		return errors.New("preflight found problems: nothing was deployed")
	}
	fmt.Fprintln(w, "    VM sizes, zones and quota fit")
	return nil
}
