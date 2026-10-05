// Package deploy runs the infra layers of the enabled modules as Azure deployment stacks, in
// order, and records their outputs in .bluepave/discovered.yaml (ADR-0001, ADR-0002).
package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/kushan94/bluepave/internal/config"
	"github.com/kushan94/bluepave/internal/discovered"
	"github.com/kushan94/bluepave/internal/modules"
	"github.com/kushan94/bluepave/internal/run"
)

// Stack is one module's infra layer in one environment ("" for a global module).
type Stack struct {
	Module   *modules.Module
	Env      string
	Name     string // the deployment stack's name
	Template string // path to the module's infra entry point
}

// Label is how the stack is shown: <module> or <env>/<module>.
func (s Stack) Label() string {
	if s.Env == "" {
		return s.Module.Metadata.Name
	}
	return s.Env + "/" + s.Module.Metadata.Name
}

// Plan returns the stacks to deploy, in order: global modules first (once per subscription), then
// every environment's modules, each in the resolved install order. Modules without an infra layer
// are skipped.
func Plan(p *config.Platform, ordered []*modules.Module) []Stack {
	prefix := p.Spec.Prefix
	var out []Stack
	for _, m := range ordered {
		if t, ok := m.Spec.Layers["infra"]; ok && m.Spec.InfraScope == "global" {
			out = append(out, Stack{Module: m, Name: fmt.Sprintf("bp-%s-%s", prefix, m.Metadata.Name), Template: filepath.Join(m.Dir, t)})
		}
	}
	for _, env := range p.Spec.Environments {
		for _, m := range ordered {
			if t, ok := m.Spec.Layers["infra"]; ok && m.Spec.InfraScope != "global" {
				out = append(out, Stack{Module: m, Env: env, Name: fmt.Sprintf("bp-%s-%s-%s", prefix, env, m.Metadata.Name), Template: filepath.Join(m.Dir, t)})
			}
		}
	}
	return out
}

// Engine deploys stacks with the Azure CLI.
type Engine struct {
	Runner   run.Runner
	Location string // where the stack objects themselves live (the platform's region)
}

// Parameters returns the stack's parameters: environmentName for environment-scoped modules, and,
// for every parameter the template declares that matches one of the module's recorded outputs,
// that output (sticky values chosen on the first deployment, like the budgets' start date).
func (e Engine) Parameters(ctx context.Context, s Stack, ids discovered.IDs) ([]string, error) {
	declared, err := e.templateParameters(ctx, s.Template)
	if err != nil {
		return nil, err
	}
	var params []string
	if s.Env != "" {
		params = append(params, "environmentName="+s.Env)
	}
	recorded := ids.Outputs(s.Env, s.Module.Metadata.Name)
	names := make([]string, 0, len(recorded))
	for k := range recorded {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if k != "environmentName" && declared[k] {
			if v, ok := recorded[k].(string); ok {
				params = append(params, k+"="+v)
			}
		}
	}
	return params, nil
}

func (e Engine) templateParameters(ctx context.Context, template string) (map[string]bool, error) {
	out, err := e.Runner.Run(ctx, "az", "bicep", "build", "--file", template, "--stdout")
	if err != nil {
		return nil, err
	}
	var compiled struct {
		Parameters map[string]any `json:"parameters"`
	}
	if len(out) > 0 {
		if err := json.Unmarshal(out, &compiled); err != nil {
			return nil, fmt.Errorf("%s: compiled template: %w", template, err)
		}
	}
	declared := map[string]bool{}
	for k := range compiled.Parameters {
		declared[k] = true
	}
	return declared, nil
}

// WhatIf previews the stack's changes. It runs a subscription deployment what-if of the same
// template and parameters: the Azure CLI (2.90) has no `az stack sub what-if` yet.
func (e Engine) WhatIf(ctx context.Context, s Stack, ids discovered.IDs) ([]byte, error) {
	params, err := e.Parameters(ctx, s, ids)
	if err != nil {
		return nil, err
	}
	args := []string{"deployment", "sub", "what-if", "--name", s.Name, "--location", e.Location, "--template-file", s.Template}
	if len(params) > 0 {
		args = append(args, append([]string{"--parameters"}, params...)...)
	}
	return e.Runner.Run(ctx, "az", args...)
}

// Deploy creates or updates the stack and records its outputs.
//   - action-on-unmanage deleteAll: resources removed from a module are deleted in Azure, so the
//     code stays the single source of truth.
//   - deny settings none: the portal stays usable for experiments (protecting stacks with
//     denyDelete is a later profile setting).
func (e Engine) Deploy(ctx context.Context, s Stack, ids discovered.IDs) (map[string]any, error) {
	params, err := e.Parameters(ctx, s, ids)
	if err != nil {
		return nil, err
	}
	args := []string{"stack", "sub", "create",
		"--name", s.Name, "--location", e.Location, "--template-file", s.Template,
		"--action-on-unmanage", "deleteAll", "--deny-settings-mode", "none", "--yes", "--output", "json"}
	if len(params) > 0 {
		args = append(args, append([]string{"--parameters"}, params...)...)
	}
	out, err := e.Runner.Run(ctx, "az", args...)
	if err != nil {
		return nil, err
	}
	outputs, err := parseOutputs(out)
	if err != nil {
		return nil, fmt.Errorf("stack %s: %w", s.Name, err)
	}
	ids.SetOutputs(s.Env, s.Module.Metadata.Name, outputs)
	return outputs, nil
}

// parseOutputs reads a stack's outputs: {"outputs": {"name": {"type": ..., "value": ...}}}.
func parseOutputs(out []byte) (map[string]any, error) {
	if len(out) == 0 {
		return map[string]any{}, nil
	}
	var stack struct {
		Outputs map[string]struct {
			Value any `json:"value"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(out, &stack); err != nil {
		return nil, fmt.Errorf("reading outputs: %w", err)
	}
	values := map[string]any{}
	for k, o := range stack.Outputs {
		values[k] = o.Value
	}
	return values, nil
}
