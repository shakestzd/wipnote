package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/slug"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// yoloEmitBannerFn is the seam for tests. In production it calls RenderLaunchBanner
// and writes the framed block to w; tests replace it to assert the banner fires
// without requiring a real terminal.
//
// Decision (slice 4, option b): yolo intentionally SKIPS the interactive chooser
// (autonomous mode must not block on a prompt) but ADOPTS the framed banner for
// all launch output, consistent with the claude/codex/antigravity launch paths.
var yoloEmitBannerFn = func(headline, pluginSource, session, workItem, warning string, w io.Writer) {
	in := launchtui.BannerInput{
		Headline:     headline,
		PluginSource: pluginSource,
		Session:      session,
	}
	// Fold the work item INTO the block as a detail row rather than printing a
	// separate "  Work item:" line outside the banner (feat-4cc98c24).
	if workItem != "" {
		in.Details = append(in.Details, launchtui.BannerDetail{Label: "Work item", Value: workItem})
	}
	// Fold the dirty-main advisory into the SAME boxless block (amber) so no
	// separate warning prints (bug-0f6af202). Only set on the --no-worktree path;
	// when a worktree is created the carryover message covers it and the plan's
	// DirtyMainWarning was already cleared by applyLaunchPlanOpts.
	if warning != "" {
		in.Warning = warning
		in.WarningSeverity = "amber"
	}
	fmt.Fprintln(w, launchtui.RenderLaunchBanner(nil, in))
}

// emitYoloBanner writes the boxless launch banner to w via yoloEmitBannerFn.
// The indirection through yoloEmitBannerFn allows tests to assert the banner
// fires without requiring a real terminal. pluginSource, workItem, and the
// dirty-main advisory are folded into the single block so no raw fmt lines leak
// around the banner.
func emitYoloBanner(headline, pluginSource, session, workItem, warning string, w io.Writer) {
	yoloEmitBannerFn(headline, pluginSource, session, workItem, warning, w)
}

func yoloCmd() *cobra.Command {
	var f yoloFlags

	cmd := &cobra.Command{
		Use:   "yolo",
		Short: "Launch Claude Code in autonomous YOLO mode with development guardrails",
		Long:  yoloLongHelp,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runYoloCmd(&f, args)
		},
	}

	registerYoloFlags(cmd, &f)
	return cmd
}

// yoloLongHelp is the `wipnote yolo` long help text.
const yoloLongHelp = `Launches Claude Code in auto permission mode with enforced quality guardrails.

Auto mode lets Claude Code's background classifier approve actions instead of
prompting. If auto mode is unavailable for your account or model, Claude Code
starts in manual mode and may stop for prompts. Pass
--permission-mode bypassPermissions to skip all permission checks instead.

YOLO mode avoids permission prompts but enforces code quality at every step:
  - Mandatory TDD workflow (tests before implementation)
  - Quality gate checks before every commit
  - Budget limits to keep features focused
  - Worktree-per-feature isolation

Each session is auto-named with a timestamp for easy identification.

Requires --track or --feature to identify the work item for attribution.
Without either flag, launches in planning mode to help you create one first.`

// yoloFlags holds the `wipnote yolo` flag values.
type yoloFlags struct {
	dev, initMode, continueMode, noWorktree, inPlace, tmux bool
	permMode, trackID, featureID, resumeID, name           string
}

// runYoloCmd dispatches `wipnote yolo` to the selected mode.
func runYoloCmd(f *yoloFlags, args []string) error {
	// Tmux wrap must happen before any side-effecting work.
	// When --tmux is set and we are not already inside tmux, this
	// replaces the current process with: tmux new-session -A -s wipnote-yolo -- <argv without --tmux>
	// and never returns. If tmux is missing, an error is returned.
	// If we are already inside tmux (TMUX env set), this is a no-op.
	_ = f.tmux // flag is consumed via os.Args inspection in maybeTmuxWrap
	if err := validateYoloPermissionMode(f.permMode); err != nil {
		return err
	}
	if err := maybeTmuxWrap("wipnote-yolo"); err != nil {
		return err
	}
	switch {
	case f.dev:
		effectiveInPlaceDev := f.inPlace || f.noWorktree
		return launchYoloDev(f.permMode, f.trackID, f.featureID, effectiveInPlaceDev, f.resumeID, f.name, args)
	case f.initMode:
		return launchYoloInit(f.permMode, f.trackID, f.featureID, f.resumeID, f.name, args)
	case f.continueMode:
		return launchYoloContinue(f.permMode, args, f.resumeID)
	default:
		effectiveInPlace := f.inPlace || f.noWorktree
		return launchYoloDefault(f.permMode, f.trackID, f.featureID, effectiveInPlace, f.resumeID, f.name, args)
	}
}

// registerYoloFlags registers the `wipnote yolo` flags on cmd.
func registerYoloFlags(cmd *cobra.Command, f *yoloFlags) {
	cmd.Flags().BoolVar(&f.dev, "dev", false, "Load plugin from local source (development mode)")
	cmd.Flags().BoolVar(&f.initMode, "init", false, "Initialize .wipnote/ then launch in YOLO mode")
	cmd.Flags().BoolVar(&f.continueMode, "continue", false, "Resume last YOLO session")
	cmd.Flags().BoolVar(&f.noWorktree, "no-worktree", false, "Skip worktree creation; run in project root (alias for --in-place)")
	cmd.Flags().BoolVar(&f.inPlace, "in-place", false, "Intentional in-place mutation; records opt-out of isolation")
	cmd.Flags().BoolVar(&f.tmux, "tmux", false, "Wrap yolo in a tmux session named 'wipnote-yolo' (survives disconnects; reattaches on re-run)")
	cmd.Flags().StringVar(&f.permMode, "permission-mode", yoloDefaultPermissionMode, yoloPermissionModeHelp)
	cmd.Flags().StringVar(&f.trackID, "track", "", "Track ID to work on (e.g., trk-3719d8f3)")
	cmd.Flags().StringVar(&f.featureID, "feature", "", "Feature ID to work on (e.g., feat-15c458aa)")
	cmd.Flags().StringVar(&f.resumeID, "resume", "", "Resume a specific Claude Code session by ID")
	cmd.Flags().StringVar(&f.name, "name", "", "Session label shown in Claude TUI (default: <track-title>-yolo-<timestamp>)")
}

// yoloDefaultName builds the default session label for YOLO mode.
// When a trackID or featureID is provided, it resolves the track title and
// builds "<track-slug>-yolo-<timestamp>". Falls back to "<project-slug>-yolo-<timestamp>".
func yoloDefaultName(trackID, featureID, projectRoot string) string {
	ts := time.Now().Format("20060102-15.04.05")
	trackTitle := resolveTrackTitle(trackID, featureID, projectRoot)
	if trackTitle != "" {
		s := slug.Make(trackTitle, 30)
		if s != "" {
			return s + "-yolo-" + ts
		}
	}
	// Fallback: use project basename.
	if projectRoot != "" {
		s := slug.Make(filepath.Base(projectRoot), 30)
		if s != "" {
			return s + "-yolo-" + ts
		}
	}
	return "yolo-" + ts
}

// resolveTrackTitle returns the title of the track associated with the given
// trackID or featureID. Returns empty string if not resolvable.
func resolveTrackTitle(trackID, featureID, projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	// Resolve the track ID: either direct or via feature's parent track.
	tid := trackID
	if tid == "" && featureID != "" {
		tid = resolveTrackForFeature(featureID, projectRoot)
	}
	if tid == "" {
		return ""
	}
	trackFile := filepath.Join(projectRoot, ".wipnote", "tracks", tid+".html")
	node, err := htmlparse.ParseFile(trackFile)
	if err != nil {
		return ""
	}
	return node.Title
}

// validateWorkItem checks that a track or feature HTML file exists in .wipnote/.
// Returns the validated ID and item type, or an error.
func validateWorkItem(trackID, featureID, projectRoot string) (id, kind string, err error) {
	wipnoteDir := filepath.Join(projectRoot, ".wipnote")
	switch {
	case trackID != "":
		htmlFile := filepath.Join(wipnoteDir, "tracks", trackID+".html")
		if _, statErr := os.Stat(htmlFile); os.IsNotExist(statErr) {
			return "", "", workitem.ErrNotFound("track", trackID)
		}
		return trackID, "track", nil
	case featureID != "":
		htmlFile := filepath.Join(wipnoteDir, "features", featureID+".html")
		if _, statErr := os.Stat(htmlFile); os.IsNotExist(statErr) {
			return "", "", workitem.ErrNotFound("feature", featureID)
		}
		return featureID, "feature", nil
	default:
		return "", "", nil
	}
}

// resolveTrackForFeature reads a feature HTML file and returns its data-track-id attribute.
// If the feature file doesn't exist or has no track ID, returns empty string.
func resolveTrackForFeature(featureID, projectRoot string) string {
	featureFile := filepath.Join(projectRoot, ".wipnote", "features", featureID+".html")
	node, err := htmlparse.ParseFile(featureFile)
	if err != nil {
		// File not found or parse error — gracefully return empty
		return ""
	}
	return node.TrackID
}

// buildWorkItemPromptPrefix returns the work item header to prepend to the yolo prompt.
func buildWorkItemPromptPrefix(id, _ string) string {
	return strings.Join([]string{
		"## Active Work Item",
		fmt.Sprintf("You are working on: %s", id),
		"All work in this session must be attributed to this item.",
		"",
	}, "\n")
}

// buildYoloSystemPrompt prepends the work item header to the embedded yolo prompt.
func buildYoloSystemPrompt(id, kind string) string {
	var sb strings.Builder
	if id != "" {
		sb.WriteString(buildWorkItemPromptPrefix(id, kind))
	}
	sb.WriteString(yoloPromptContent)
	// Append the shared research-routing disposition exactly once. Source of
	// truth: cmd/wipnote/prompts/research-routing.md.
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(researchRoutingContent))
	sb.WriteString("\n")
	return sb.String()
}

// launchYoloPlanningMode launches Claude in planning mode (no bypass permissions)
// when no --track or --feature is provided. Prints guidance before launching.
func launchYoloPlanningMode(projectRoot string, extraArgs []string) error {
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline: "No --track/--feature — launching YOLO planning mode...",
		Details: []launchtui.BannerDetail{
			{Value: "Create a track or feature first, then restart with:"},
			{Value: "wipnote yolo --track <track-id>"},
			{Value: "wipnote yolo --feature <feature-id>"},
		},
	}))
	return launchClaude(LaunchOpts{
		Mode:               "yolo-planning",
		InjectSystemPrompt: true,
		ExtraArgs:          extraArgs,
		ProjectRoot:        projectRoot,
	})
}

func launchYoloInit(permMode, trackID, featureID, resumeID, name string, extraArgs []string) error {
	// Initialize .wipnote/ first.
	if err := runInit(nil, nil); err != nil {
		return fmt.Errorf("init failed: %w", err)
	}
	fmt.Println()
	return launchYoloDefault(permMode, trackID, featureID, false, resumeID, name, extraArgs)
}

func launchYoloContinue(permMode string, extraArgs []string, resumeID string) error {
	projectRoot := ""
	if wipnoteDir, err := findWipnoteDir(); err == nil {
		projectRoot = filepath.Dir(wipnoteDir)
	}

	pluginDir, err := resolveBundledPluginDir()
	if err != nil {
		return err
	}
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:        fmt.Sprintf("Resuming last YOLO session (%s)...", permMode),
		PluginSource:    pluginDir,
		Warning:         yoloPermissionWarning(permMode),
		WarningSeverity: "amber",
	}))

	return launchClaude(LaunchOpts{
		Mode:           "yolo-continue",
		PluginDir:      pluginDir,
		Resume:         true,
		ResumeID:       resumeID,
		PermissionMode: permMode,
		ExtraArgs:      extraArgs,
		ProjectRoot:    projectRoot,
		ExtraEnv:       yoloLaunchEnv(),
	})
}
