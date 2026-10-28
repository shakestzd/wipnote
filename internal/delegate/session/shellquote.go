package session

import "strings"

// Quote quotes one word for a POSIX shell (sh, bash, zsh): everything stays
// literal, including $, backticks, quotes, globs and newlines.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// BootLine is the ONE line spawn types into a worker's pane:
//
//	WIPNOTE_DELEGATE_RUN=<run> [WIPNOTE_DELEGATE_CALLER=<sid>] <exe> delegate boot --project-dir <dir> <run>
//
// It carries no brief (finding 3: macOS rejects long input lines; the brief
// goes by file) and stays far below the terminal's line limit. The inherited
// session env is scrubbed by boot itself (BootEnv), so the line stays short.
func BootLine(exe, projectDir, runID, caller string) string {
	words := []string{RunEnv + "=" + Quote(runID)}
	if caller != "" {
		words = append(words, CallerEnv+"="+Quote(caller))
	}
	for _, w := range []string{exe, "delegate", "boot", "--project-dir", projectDir, runID} {
		words = append(words, Quote(w))
	}
	return strings.Join(words, " ")
}
