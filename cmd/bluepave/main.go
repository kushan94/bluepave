// Command bluepave installs and operates a bluepave platform (ADR-0001).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/kushan94/bluepave/internal/config"
	"github.com/kushan94/bluepave/internal/modules"
	"github.com/kushan94/bluepave/internal/render"
)

// version is set at release time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `bluepave: an Azure-native internal developer platform.

Usage:
  bluepave validate [-f bluepave.yaml] [-root .]   check the configuration, profile and modules
  bluepave render   [-f bluepave.yaml] [-root .] [-o file] [-check]
                                                   write .bluepave/resolved.yaml for GitOps
                                                   (-check: fail if it's out of date)
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
	case "render":
		return renderCmd(args[1:], stdout, stderr)
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

// platform is a loaded and checked configuration.
type platform struct {
	file    string
	root    string
	config  *config.Platform
	ordered []*modules.Module
}

// load reads bluepave.yaml and its profile, resolves the enabled modules and checks their
// settings. Every check runs offline.
func load(file, root string) (*platform, error) {
	p, err := config.LoadPlatform(file)
	if err != nil {
		return nil, err
	}
	prof, err := config.LoadProfile(root, p.Spec.Profile)
	if err != nil {
		return nil, err
	}
	available, err := modules.Discover(root)
	if err != nil {
		return nil, err
	}
	ordered, err := modules.Resolve(available, config.EnabledModules(p, prof))
	if err != nil {
		return nil, err
	}
	var problems []error
	for _, m := range ordered {
		if err := m.ValidateSettings(p.Spec.Modules[m.Metadata.Name].Settings); err != nil {
			problems = append(problems, err)
		}
	}
	if _, err := config.LoadApps(root, p); err != nil {
		problems = append(problems, err)
	}
	// A platform hostname belongs to one module: two listeners for one hostname would be ambiguous.
	hostOwner := map[string]string{}
	for _, m := range ordered {
		for _, h := range m.Spec.Hostnames {
			if other, taken := hostOwner[h.Name]; taken {
				problems = append(problems, fmt.Errorf("modules %q and %q both claim the platform hostname %q", other, m.Metadata.Name, h.Name))
				continue
			}
			hostOwner[h.Name] = m.Metadata.Name
		}
	}
	// Settings for a module that isn't enabled are most likely a typo in its name.
	on := map[string]bool{}
	for _, m := range ordered {
		on[m.Metadata.Name] = true
	}
	for _, n := range sortedOverrides(p.Spec.Modules) {
		if o := p.Spec.Modules[n]; len(o.Settings) > 0 && !on[n] {
			problems = append(problems, fmt.Errorf("spec.modules.%s has settings, but the module isn't enabled", n))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return nil, err
	}
	return &platform{file: file, root: root, config: p, ordered: ordered}, nil
}

func validate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "bluepave.yaml", "platform configuration")
	root := fs.String("root", ".", "bluepave repository root (profiles/, modules/)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pl, err := load(*file, *root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	p := pl.config
	fmt.Fprintf(stdout, "%s is valid: platform %q, profile %s, region %s, environments %v\n",
		filepath.Base(*file), p.Metadata.Name, p.Spec.Profile, p.Spec.Azure.Region, p.Spec.Environments)
	if len(pl.ordered) == 0 {
		fmt.Fprintln(stdout, "modules: none enabled yet")
		return 0
	}
	fmt.Fprintln(stdout, "modules, in install order:")
	for _, m := range pl.ordered {
		fmt.Fprintf(stdout, "  %-28s %s\n", m.Metadata.Name, m.Spec.Version)
	}
	return 0
}

func renderCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "bluepave.yaml", "platform configuration")
	root := fs.String("root", ".", "bluepave repository root")
	check := fs.Bool("check", false, "fail if the file on disk is out of date, instead of writing it")
	out := fs.String("o", "", "output file (default <root>/"+render.Path+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pl, err := load(*file, *root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	path := *out
	if path == "" {
		path = filepath.Join(pl.root, render.Path)
	}
	r, err := render.Build(pl.root, pl.config, pl.ordered)
	if err == nil {
		err = r.Write(path, *check)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *check {
		fmt.Fprintf(stdout, "%s is up to date\n", path)
	} else {
		fmt.Fprintf(stdout, "wrote %s (%d modules)\n", path, len(r.Modules))
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

func sortedOverrides(m map[string]config.ModuleOverride) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
