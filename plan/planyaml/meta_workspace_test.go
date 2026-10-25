package planyaml

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidate_MetaWorkspaceFieldsOptional(t *testing.T) {
	if errs := Validate(validPlan()); len(errs) != 0 {
		t.Fatalf("plan without workspace fields: %v", errs)
	}
	p := validPlan()
	p.Meta.Repositories = []string{"shakestzd/wipnote", "shakestzd/other"}
	p.Meta.BaseBranch = "feature/delegate"
	p.Meta.PRNaming = "<work-item-id>: <title>"
	p.Meta.Gates = []string{"go build ./...", "go test ./..."}
	if errs := Validate(p); len(errs) != 0 {
		t.Fatalf("plan with valid workspace fields: %v", errs)
	}
}

func TestValidate_MetaWorkspaceFieldsRejectBad(t *testing.T) {
	cases := map[string]func(*PlanYAML){
		"meta.repositories[1]": func(p *PlanYAML) { p.Meta.Repositories = []string{"a/b", " "} },
		"duplicate":            func(p *PlanYAML) { p.Meta.Repositories = []string{"a/b", "a/b"} },
		"host-local":           func(p *PlanYAML) { p.Meta.Repositories = []string{"/Users/alice/src/x"} },
		"meta.base_branch":     func(p *PlanYAML) { p.Meta.BaseBranch = "main branch" },
		"meta.pr_naming":       func(p *PlanYAML) { p.Meta.PRNaming = "see /home/bob" },
		"meta.gates[0]":        func(p *PlanYAML) { p.Meta.Gates = []string{""} },
	}
	for want, mutate := range cases {
		p := validPlan()
		mutate(p)
		errs := Validate(p)
		if len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), want) {
			t.Errorf("%s: errs = %v", want, errs)
		}
	}
}

func TestMetaWorkspaceFields_YAMLRoundTrip(t *testing.T) {
	p := validPlan()
	out, _ := yaml.Marshal(p)
	for _, key := range []string{"repositories:", "base_branch:", "pr_naming:", "gates:"} {
		if strings.Contains(string(out), key) {
			t.Fatalf("empty %s should be omitted", key)
		}
	}
	p.Meta.Repositories = []string{"a/b"}
	p.Meta.BaseBranch = "main"
	p.Meta.PRNaming = "feat: x"
	p.Meta.Gates = []string{"make test"}
	out, _ = yaml.Marshal(p)
	back, err := LoadBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	m := back.Meta
	if len(m.Repositories) != 1 || m.BaseBranch != "main" || m.PRNaming != "feat: x" || len(m.Gates) != 1 {
		t.Fatalf("round trip lost fields: %+v", m)
	}
}
