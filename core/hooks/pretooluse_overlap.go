package hooks

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/paths"
)

// fileOverlapConfig holds the two opt-in flags read from .wipnote/config.json
// via the same local os.ReadFile pattern as readTaskCompletionConfig (there is
// NO shared internal/config package). warn defaults to true, block to false —
// so the JSON pointers distinguish "absent" (use default) from "explicitly
// set" (honor the value).
type fileOverlapConfig struct {
	WarnOnFileOverlap  *bool `json:"warn_on_file_overlap"`
	BlockOnFileOverlap *bool `json:"block_on_file_overlap"`
}

// readFileOverlapConfig returns (warn, block) for the live file-overlap
// advisory. Defaults: warn=true, block=false. A missing/unreadable config or
// absent key keeps the defaults; an explicit false in config disables warning.
func readFileOverlapConfig(projectDir string) (warn, block bool) {
	warn, block = true, false
	if projectDir == "" {
		return warn, block
	}
	var cfg fileOverlapConfig
	if !paths.ReadProjectConfig(projectDir, &cfg) {
		return warn, block
	}
	if cfg.WarnOnFileOverlap != nil {
		warn = *cfg.WarnOnFileOverlap
	}
	if cfg.BlockOnFileOverlap != nil {
		block = *cfg.BlockOnFileOverlap
	}
	return warn, block
}

// checkFileOverlapAdvisory performs the single-SELECT live file-overlap probe
// for Write/Edit/MultiEdit. Returns (advisory, nil) for the non-blocking warn
// path, ("", &BlockExit2Error) for the blocking path, or ("", nil) when there
// is no overlap or the feature is disabled. ZERO writes on every path.
func checkFileOverlapAdvisory(event *CloudEvent, ctx *toolUseContext, database *sql.DB) (string, error) {
	switch event.ToolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return "", nil
	}
	if ctx == nil || ctx.SessionID == "" || database == nil {
		return "", nil
	}
	warn, block := readFileOverlapConfig(ctx.ProjectDir)
	if !warn && !block {
		return "", nil
	}

	rawPath := extractFilePath(event.ToolInput)
	if rawPath == "" {
		return "", nil
	}
	// feature_files stores repo-relative paths (recordEventAndAllow uses the
	// same MustNormalize call), so normalize before matching.
	target := paths.MustNormalize(rawPath, "")

	window := db.FileOverlapWindow(ctx.ProjectDir)
	liveness := db.LivenessStalenessThreshold(ctx.ProjectDir)
	overlaps, err := db.FindLiveFileOverlaps(database, target, ctx.SessionID, window, liveness)
	if err != nil || len(overlaps) == 0 {
		return "", nil
	}

	sessions := describeOverlapSessions(overlaps)

	if block {
		return "", &BlockExit2Error{Message: fmt.Sprintf(
			"File-overlap block: %s was touched within the last %s by another "+
				"live session: %s.\n"+
				"Recovery: coordinate with the other session, or re-run after it "+
				"completes, or ask the operator to relax block_on_file_overlap in "+
				".wipnote/config.json.",
			target, window.String(), sessions),
		}
	}
	return fmt.Sprintf(
		"⚠ wipnote: %s was also touched within the last %s by another live "+
			"session: %s. You may be editing the same file concurrently — "+
			"coordinate to avoid clobbering each other's changes.",
		target, window.String(), sessions), nil
}

// describeOverlapSessions renders the overlapping sessions as a
// comma-separated list of "session (feature)" descriptions.
func describeOverlapSessions(overlaps []db.FileOverlap) string {
	var others []string
	for _, o := range overlaps {
		desc := o.SessionID
		if o.FeatureID != "" {
			desc += " (" + o.FeatureID + ")"
		}
		others = append(others, desc)
	}
	return strings.Join(others, ", ")
}
