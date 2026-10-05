// Command bluepave installs and operates a bluepave platform (ADR-0001).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/kushan94/bluepave/internal/config"
	"github.com/kushan94/bluepave/internal/modules"
)

// version is set at release time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `bluepave: an Azure-native internal developer platform.

Usage:
  bluepave validate [-f bluepave.yaml] [-root .]   check the configuration, profile and modules
  bluepave modules  [-root .]                      list the available modules
  bluepave version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "validate":
		return validate(args[1:], stdout, stderr)
	case "modules":
		return listModules(args[1:], stdout, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func validate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "bluepave.yaml", "platform configuration")
	root := fs.String("root", ".", "bluepave repository root (profiles/, modules/)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	p, err := config.LoadPlatform(*file)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	prof, err := config.LoadProfile(*root, p.Spec.Profile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	available, err := modules.Discover(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ordered, err := modules.Resolve(available, config.EnabledModules(p, prof))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s is valid: platform %q, profile %s, region %s, environments %v\n",
		filepath.Base(*file), p.Metadata.Name, p.Spec.Profile, p.Spec.Azure.Region, p.Spec.Environments)
	if len(ordered) == 0 {
		fmt.Fprintln(stdout, "modules: none enabled yet")
		return 0
	}
	fmt.Fprintln(stdout, "modules, in install order:")
	for _, m := range ordered {
		fmt.Fprintf(stdout, "  %-28s %s\n", m.Metadata.Name, m.Spec.Version)
	}
	return 0
}

func listModules(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("modules", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "bluepave repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	available, err := modules.Discover(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(available) == 0 {
		fmt.Fprintln(stdout, "no modules yet")
		return 0
	}
	for _, n := range sortedKeys(available) {
		m := available[n]
		fmt.Fprintf(stdout, "%-28s %-8s %s\n", n, m.Spec.Version, m.Metadata.Description)
	}
	return 0
}

func sortedKeys(m map[string]*modules.Module) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
