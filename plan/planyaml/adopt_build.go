package planyaml

import (
	"fmt"
	"strings"
)

// AdoptVsBuild records a slice's adopt-vs-build decision for a new module.
// Decision "adopt" must name Package; "build" must list the Rejected
// candidates that were evaluated, each with the reason it was rejected.
type AdoptVsBuild struct {
	Decision  string   `yaml:"decision"`            // adopt | build
	Package   string   `yaml:"package,omitempty"`   // adopted package (adopt)
	Rejected  []string `yaml:"rejected,omitempty"`  // "candidate: reason" (build)
	Rationale string   `yaml:"rationale,omitempty"` // optional free text
}

var newModuleExts = []string{".go", ".rs", ".py", ".ts", ".tsx", ".js", ".java", ".kt", ".swift", ".c", ".cc", ".cpp", ".rb"}

// sliceCreatesModule reports whether the slice's declared files create a new
// non-test source module: an entry marked "(new)" or "new:" that is a source
// file, or a new package directory (trailing "/"). Docs and tests don't count.
func sliceCreatesModule(files []string) bool {
	for _, f := range files {
		f = strings.TrimSpace(f)
		isNew := strings.HasSuffix(f, "(new)") || strings.HasPrefix(f, "new:")
		if !isNew {
			continue
		}
		f = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(f, "(new)"), "new:"))
		if strings.HasSuffix(f, "/") {
			return true
		}
		if strings.Contains(f, "_test.") || strings.Contains(f, ".test.") {
			continue
		}
		for _, ext := range newModuleExts {
			if strings.HasSuffix(f, ext) {
				return true
			}
		}
	}
	return false
}

// adoptBuildProblems returns what is wrong with the slice's adopt-vs-build
// decision for a new module, or nil when the decision is complete. Trivial
// slices and slices creating no module have nothing to check.
func adoptBuildProblems(prefix string, s PlanSlice) []string {
	if s.Complexity == "trivial" || !sliceCreatesModule(s.Files) {
		return nil
	}
	a := s.AdoptVsBuild
	if a == nil {
		return []string{prefix + ".adopt_vs_build required: slice creates a new module; name the adopted package or the rejected candidates"}
	}
	switch a.Decision {
	case "adopt":
		if strings.TrimSpace(a.Package) == "" {
			return []string{prefix + ".adopt_vs_build.package must name the adopted package"}
		}
	case "build":
		if len(a.Rejected) == 0 {
			return []string{prefix + ".adopt_vs_build.rejected must list the evaluated and rejected candidates"}
		}
	default:
		return []string{fmt.Sprintf("%s.adopt_vs_build.decision %q must be adopt or build", prefix, a.Decision)}
	}
	return nil
}

// adoptBuildEnforced: the rule blocks only v4 plans that are not yet
// finalized. Finalized, active and completed plans get advisories instead.
func adoptBuildEnforced(plan *PlanYAML) bool {
	if plan.Meta.SchemaVersion != "v4" {
		return false
	}
	switch plan.Meta.Status {
	case "finalized", "active", "completed":
		return false
	}
	return true
}

func validateSliceAdoptBuild(plan *PlanYAML, prefix string, s PlanSlice) []string {
	if !adoptBuildEnforced(plan) {
		return nil
	}
	return adoptBuildProblems(prefix, s)
}

// ValidateAdoptBuildAdvisories returns NON-BLOCKING notes for finalized v4
// plans whose new-module slices lack a complete adopt-vs-build decision.
func ValidateAdoptBuildAdvisories(plan *PlanYAML) []string {
	if plan.Meta.SchemaVersion != "v4" || adoptBuildEnforced(plan) {
		return nil
	}
	var adv []string
	for i, s := range plan.Slices {
		adv = append(adv, adoptBuildProblems(fmt.Sprintf("slices[%d] (%s)", i, s.ID), s)...)
	}
	return adv
}
