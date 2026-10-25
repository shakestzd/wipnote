package planyaml

import (
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/core/hostpath"
)

// validateMetaWorkspace checks the optional workspace fields on meta
// (repositories, base_branch, pr_naming, gates). Absent fields are valid; a
// field that is present must be non-blank and hold no host-local path, and a
// branch name holds no whitespace.
func validateMetaWorkspace(m PlanMeta) []string {
	var errs []string
	errs = append(errs, validateTextList("meta.repositories", m.Repositories, true)...)
	errs = append(errs, validateTextList("meta.gates", m.Gates, false)...)
	if m.BaseBranch != "" {
		if strings.TrimSpace(m.BaseBranch) == "" || strings.ContainsAny(m.BaseBranch, " \t\r\n") {
			errs = append(errs, fmt.Sprintf("meta.base_branch %q must be a branch name without spaces", m.BaseBranch))
		}
	}
	if m.PRNaming != "" && strings.TrimSpace(m.PRNaming) == "" {
		errs = append(errs, "meta.pr_naming must not be blank when set")
	}
	for _, f := range [][2]string{{"meta.base_branch", m.BaseBranch}, {"meta.pr_naming", m.PRNaming}} {
		if err := hostpath.Check(f[0], f[1]); err != nil {
			errs = append(errs, err.Error())
		}
	}
	return errs
}

// validateTextList refuses blank entries and host-local paths, and, when
// unique is set, duplicate entries.
func validateTextList(field string, values []string, unique bool) []string {
	var errs []string
	seen := map[string]bool{}
	for i, v := range values {
		name := fmt.Sprintf("%s[%d]", field, i)
		v = strings.TrimSpace(v)
		switch {
		case v == "":
			errs = append(errs, name+" must not be blank")
		case unique && seen[v]:
			errs = append(errs, fmt.Sprintf("%s %q is a duplicate", name, v))
		}
		seen[v] = true
		if err := hostpath.Check(name, v); err != nil {
			errs = append(errs, err.Error())
		}
	}
	return errs
}
