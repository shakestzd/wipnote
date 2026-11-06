package planyaml

import "testing"

func newModuleSlicePlan(status string) *PlanYAML {
	p := validV4Plan()
	p.Meta.Status = status
	p.Slices[0].Files = []string{"internal/sse/parser.go (new)", "internal/sse/parser_test.go (new)"}
	return p
}

func TestAdoptBuild_NewModuleWithoutDecisionRejected(t *testing.T) {
	if !errsContain(Validate(newModuleSlicePlan("draft")), "adopt_vs_build") {
		t.Errorf("new module without adopt_vs_build must fail")
	}
}

func TestAdoptBuild_AdoptCitesPackage(t *testing.T) {
	p := newModuleSlicePlan("draft")
	p.Slices[0].AdoptVsBuild = &AdoptVsBuild{Decision: "adopt", Package: "github.com/r3labs/sse/v2"}
	if errsContain(Validate(p), "adopt_vs_build") {
		t.Errorf("adopt with package must pass: %v", Validate(p))
	}
	p.Slices[0].AdoptVsBuild.Package = ""
	if !errsContain(Validate(p), "adopt_vs_build.package") {
		t.Errorf("adopt without package must fail")
	}
}

func TestAdoptBuild_BuildNeedsRejectedCandidates(t *testing.T) {
	p := newModuleSlicePlan("draft")
	p.Slices[0].AdoptVsBuild = &AdoptVsBuild{Decision: "build"}
	if !errsContain(Validate(p), "adopt_vs_build.rejected") {
		t.Errorf("build without rejected candidates must fail")
	}
	p.Slices[0].AdoptVsBuild.Rejected = []string{"r3labs/sse: no CRLF support"}
	if errsContain(Validate(p), "adopt_vs_build") {
		t.Errorf("build with rejected must pass: %v", Validate(p))
	}
}

func TestAdoptBuild_ExistingFilesAndTestsNotNewModule(t *testing.T) {
	p := validV4Plan()
	p.Slices[0].Files = []string{"internal/sse/parser.go", "docs/x.md (new)", "a_test.go (new)"}
	if errsContain(Validate(p), "adopt_vs_build") {
		t.Errorf("only (new) source files count: %v", Validate(p))
	}
}

func TestAdoptBuild_FinalizedPlanOnlyAdvisory(t *testing.T) {
	p := newModuleSlicePlan("finalized")
	if errsContain(Validate(p), "adopt_vs_build") {
		t.Errorf("finalized plans must still validate")
	}
	if len(ValidateAdoptBuildAdvisories(p)) == 0 {
		t.Errorf("finalized plan should get an advisory")
	}
}

func TestAdoptBuild_NonV4Exempt(t *testing.T) {
	p := newModuleSlicePlan("draft")
	p.Meta.SchemaVersion = "v3"
	if errsContain(Validate(p), "adopt_vs_build") {
		t.Errorf("v3 exempt")
	}
}
