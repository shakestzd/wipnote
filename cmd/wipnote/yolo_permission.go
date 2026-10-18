package main

import (
	"fmt"
	"slices"
	"strings"
)

// yoloDefaultPermissionMode is the Claude Code permission mode `wipnote yolo`
// launches with. Auto mode lets the background classifier approve actions;
// bypassPermissions is still available, but only as an explicit opt-in.
const yoloDefaultPermissionMode = "auto"

// yoloPermissionModes are the values `claude --permission-mode` accepts:
// `claude --help` lists manual, and the permissions docs name default as the
// canonical key with manual as its alias, so both are accepted.
var yoloPermissionModes = []string{"acceptEdits", "auto", "bypassPermissions", "default", "manual", "dontAsk", "plan"}

// yoloPermissionModeHelp is the --permission-mode flag help text.
var yoloPermissionModeHelp = fmt.Sprintf(
	"Claude Code permission mode (%s); pass bypassPermissions to skip all prompts",
	strings.Join(yoloPermissionModes, ", "))

// validateYoloPermissionMode rejects a mode Claude Code would not accept, so a
// typo fails before any worktree is created.
func validateYoloPermissionMode(mode string) error {
	if slices.Contains(yoloPermissionModes, mode) {
		return nil
	}
	return fmt.Errorf("invalid --permission-mode %q (allowed: %s)",
		mode, strings.Join(yoloPermissionModes, ", "))
}

// yoloAutoModeWarning is the banner warning shown when yolo runs in auto mode.
const yoloAutoModeWarning = "If auto mode is unavailable for this account or model, Claude Code starts in manual mode, " +
	"so unattended runs may stop for prompts; pass --permission-mode bypassPermissions to opt out."

// yoloPermissionWarning returns the banner warning for mode ("" when none).
func yoloPermissionWarning(mode string) string {
	if mode == "auto" {
		return yoloAutoModeWarning
	}
	return ""
}

// joinYoloWarnings folds the permission warning and any other banner warning
// (e.g. the dirty-main advisory) into the banner's single warning slot.
func joinYoloWarnings(warnings ...string) string {
	var parts []string
	for _, w := range warnings {
		if w != "" {
			parts = append(parts, w)
		}
	}
	return strings.Join(parts, " ")
}

// yoloLaunchEnv marks the session as YOLO for hook subprocesses. The hooks'
// YOLO guardrails otherwise key off permission_mode == bypassPermissions,
// which an auto-mode yolo session never reports; WIPNOTE_YOLO=1 keeps the
// guardrails on whatever permission mode yolo was launched with.
func yoloLaunchEnv() []string {
	return []string{"WIPNOTE_YOLO=1"}
}
