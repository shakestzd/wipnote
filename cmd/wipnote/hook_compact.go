package main

import (
	"fmt"
	"os"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/spf13/cobra"
)

// hookPreCompactCmd handles the PreCompact event (feat-9bc8e611). Claude Code
// uses a PreCompact hook's plain stdout as instructions for the compaction
// summary, so on Claude it prints bare text, bypassing the JSON HookResult
// layer; printing JSON there would put JSON into the summary instructions.
// Codex ignores plain stdout for PreCompact, so any other harness gets its
// normal allow response. Compaction is never blocked: every failure is logged
// and the hook exits 0.
func hookPreCompactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-compact",
		Short: "Handle PreCompact event — snapshot the resume brief and steer the compaction summary",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := hooks.ReadRawStdin()
			if err != nil {
				hooks.LogError("pre-compact", "", fmt.Sprintf("read stdin: %v", err))
				return nil
			}
			return runPreCompact(raw)
		},
	}
}

// runPreCompact decodes one PreCompact payload and writes the harness-shaped
// response to stdout.
func runPreCompact(raw []byte) error {
	harness := hooks.DetectHarnessWithOverride(raw, hookHarnessOverride)
	allow := func(event *hooks.CloudEvent) error {
		if harness == hooks.HarnessClaude {
			return nil // an empty stdout adds no summary instructions
		}
		return hooks.WriteResultForHarnessEvent(harness, hookEventNameForResponse("pre-compact", event), hooks.AllowForHarness(harness))
	}

	event, err := hooks.ParseEventForHarness(harness, raw)
	if err != nil {
		hooks.LogError("pre-compact", "", fmt.Sprintf("parse event (%s): %v", harness, err))
		return allow(nil)
	}
	if os.Getenv(headlessHookEnv) == "1" {
		return allow(event)
	}
	hooks.TraceInvocation("pre-compact", raw, event)

	projectDir := hooks.ResolveProjectDir(event.CWD, event.SessionID)
	if !hooks.IswipnoteProject(projectDir) {
		return allow(event)
	}
	instructions := hooks.PreCompact(event, projectDir)
	if instructions == "" || harness != hooks.HarnessClaude {
		return allow(event)
	}
	_, err = fmt.Fprintln(os.Stdout, instructions)
	return err
}
