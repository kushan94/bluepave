package deploy

import (
	"context"
	"strings"
	"testing"

	"github.com/kushan94/bluepave/internal/config"
	"github.com/kushan94/bluepave/internal/discovered"
	"github.com/kushan94/bluepave/internal/modules"
	"github.com/kushan94/bluepave/internal/run"
)

func module(name, scope string, infra bool) *modules.Module {
	m := &modules.Module{Dir: "modules/" + name}
	m.Metadata.Name = name
	m.Spec.InfraScope = scope
	m.Spec.Layers = map[string]string{"docs": "docs"}
	if infra {
		m.Spec.Layers["infra"] = "infra/main.bicep"
	}
	return m
}

func platform() *config.Platform {
	p := &config.Platform{}
	p.Spec.Prefix = "acme"
	p.Spec.Environments = []string{"dev", "prod"}
	return p
}

func TestPlan(t *testing.T) {
	ordered := []*modules.Module{
		module("monitoring", "environment", true),
		module("dns", "global", true),
		module("rollouts", "environment", false), // no infra layer: skipped
		module("aks", "environment", true),
	}
	var got []string
	for _, s := range Plan(platform(), ordered) {
		got = append(got, s.Label()+"="+s.Name)
	}
	want := "dns=bp-acme-dns dev/monitoring=bp-acme-dev-monitoring dev/aks=bp-acme-dev-aks " +
		"prod/monitoring=bp-acme-prod-monitoring prod/aks=bp-acme-prod-aks"
	if strings.Join(got, " ") != want {
		t.Errorf("plan =\n%s\nwant\n%s", strings.Join(got, " "), want)
	}
}

func TestDeployPassesRecordedOutputsBackAndRecordsNewOnes(t *testing.T) {
	gov := Stack{Module: module("governance", "global", true), Name: "bp-acme-governance", Template: "modules/governance/infra/main.bicep"}
	rec := &run.Recorder{Responses: map[string]string{
		// The template declares budgetStartDate, not the other recorded output.
		"az bicep build": `{"parameters": {"budgetStartDate": {"type": "string"}, "inheritedTags": {"type": "array"}}}`,
		"az stack sub create": `{"outputs": {"budgetStartDate": {"type": "String", "value": "2026-10-01"},
		                                     "other": {"type": "String", "value": "x"}}}`,
	}}
	ids := discovered.IDs{}
	ids.SetOutputs("", "governance", map[string]any{"budgetStartDate": "2026-10-01", "unrelated": "y"})
	eng := Engine{Runner: rec, Location: "westeurope"}
	outputs, err := eng.Deploy(context.Background(), gov, ids)
	if err != nil {
		t.Fatal(err)
	}
	create := rec.Calls[len(rec.Calls)-1].String()
	for _, want := range []string{
		"az stack sub create --name bp-acme-governance --location westeurope",
		"--action-on-unmanage deleteAll --deny-settings-mode none --yes",
		"--parameters budgetStartDate=2026-10-01",
	} {
		if !strings.Contains(create, want) {
			t.Errorf("command lacks %q:\n%s", want, create)
		}
	}
	if strings.Contains(create, "unrelated=") || strings.Contains(create, "environmentName=") {
		t.Errorf("unexpected parameters:\n%s", create)
	}
	if outputs["other"] != "x" || ids.Outputs("", "governance")["other"] != "x" {
		t.Errorf("outputs not recorded: %v / %v", outputs, ids.Outputs("", "governance"))
	}
}

func TestEnvironmentStackGetsEnvironmentName(t *testing.T) {
	s := Stack{Module: module("aks", "environment", true), Env: "dev", Name: "bp-acme-dev-aks", Template: "t.bicep"}
	rec := &run.Recorder{Responses: map[string]string{"az bicep build": `{"parameters": {"environmentName": {}}}`}}
	params, err := Engine{Runner: rec}.Parameters(context.Background(), s, discovered.IDs{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(params, " ") != "environmentName=dev" {
		t.Errorf("params = %v", params)
	}
}
