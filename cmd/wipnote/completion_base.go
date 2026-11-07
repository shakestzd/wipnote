package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
)

// completionBase is the .wipnote/config.json "completion_base" setting (env
// WIPNOTE_COMPLETION_BASE overrides it): which default branch a linked commit
// must be reachable from for the merged-upstream completion gate (GH #330).
type completionBase string

const (
	// completionBaseUpstream accepts only the upstream default ref.
	completionBaseUpstream completionBase = "upstream"
	// completionBaseLocal accepts the local default branch or upstream.
	completionBaseLocal completionBase = "local"
	// completionBaseAuto (default) is upstream, plus the local default branch
	// while it is ahead of upstream (merged locally, not pushed).
	completionBaseAuto completionBase = "auto"

	completionBaseEnv = "WIPNOTE_COMPLETION_BASE"
)

type completionBaseConfig struct {
	CompletionBase string `json:"completion_base"`
}

// resolveCompletionBase reads env, then project config, then defaults to auto.
// An unknown value warns on stderr and falls back to auto.
func resolveCompletionBase(projectDir string) completionBase {
	raw, source := strings.TrimSpace(os.Getenv(completionBaseEnv)), completionBaseEnv
	if raw == "" {
		var cfg completionBaseConfig
		paths.ReadProjectConfig(projectDir, &cfg)
		raw, source = strings.TrimSpace(cfg.CompletionBase), "completion_base"
	}
	switch b := completionBase(strings.ToLower(raw)); b {
	case "":
		return completionBaseAuto
	case completionBaseUpstream, completionBaseLocal, completionBaseAuto:
		return b
	}
	fmt.Fprintf(os.Stderr, "warning: unknown %s value %q (want upstream, local or auto); using auto\n", source, raw)
	return completionBaseAuto
}

// localDefaultBranch returns the short name of the local default branch
// (main, else master) or "" when neither exists.
func localDefaultBranch(root string) string {
	for _, b := range []string{"main", "master"} {
		if gitRefExists(root, "refs/heads/"+b) {
			return b
		}
	}
	return ""
}

// localAheadOfUpstream reports whether the local branch has commits that
// upstream lacks.
func localAheadOfUpstream(root, branch, upstream string) bool {
	out, err := gitOutputIn(root, "rev-list", "--count", upstream+"..refs/heads/"+branch)
	return err == nil && strings.TrimSpace(out) != "0" && strings.TrimSpace(out) != ""
}

// completionRefs lists the refs a linked commit may be reachable from under
// base: always upstream, plus the local default branch per the setting.
func completionRefs(root, upstream string, base completionBase) []string {
	refs := []string{upstream}
	local := localDefaultBranch(root)
	if local == "" || base == completionBaseUpstream {
		return refs
	}
	if base == completionBaseLocal || localAheadOfUpstream(root, local, upstream) {
		refs = append(refs, "refs/heads/"+local)
	}
	return refs
}
