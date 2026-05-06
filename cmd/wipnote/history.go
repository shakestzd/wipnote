package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// historyEntry holds a single git log record for a work-item file.
type historyEntry struct {
	SHA     string `json:"sha"`
	ISOTime string `json:"iso_time"`
	Author  string `json:"author"`
	Subject string `json:"subject"`
}

// newHistoryCmd returns the cobra command for `htmlgraph history <id>`.
func newHistoryCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "history <id>",
		Short: "Show the git commit history for a work-item file",
		Long: `Resolves a work-item ID to its HTML (or YAML) file and prints
the git log for that file, most-recent commit first.

Supported prefixes: feat-, bug-, spk-, plan-, trk-

Examples:
  htmlgraph history feat-2a43f5f8
  htmlgraph history plan-3b0d5133 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runHistory(args[0], jsonOut)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON array of log entries")
	return cmd
}

// runHistory is the top-level handler: resolves the path, runs git log, and
// renders the result.
func runHistory(id string, jsonOut bool) error {
	hgDir, err := findHtmlgraphDir()
	if err != nil {
		return err
	}

	path, err := resolveHistoryPath(hgDir, id)
	if err != nil {
		return err
	}

	// Resolve the right git toplevel for `git log`.
	//
	// Two failure modes to avoid:
	//   - nested submodule: cwd is inside a different repository than the
	//     discovered .wipnote, and using cwd would log the submodule.
	//   - linked worktree: .wipnote lives in the main checkout but cwd is an
	//     active linked worktree; using the .wipnote owner would log the
	//     main checkout's HEAD and miss branch-local history.
	//
	// Strategy: prefer cwd's worktree if it belongs to the SAME repository as
	// the .wipnote owner (same git-common-dir), otherwise fall back to the
	// .wipnote owner. git-common-dir is shared by every linked worktree of
	// a repo and differs for submodules, so it is the correct discriminator.
	repoRoot, err := resolveHistoryRoot(filepath.Dir(hgDir))
	if err != nil {
		repoRoot = filepath.Dir(hgDir)
	}

	entries, err := runHistoryLog(repoRoot, path)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No commits found for %s\nIs this file tracked by git?\n", id)
		return nil
	}

	if jsonOut {
		return renderHistoryJSON(entries)
	}
	return renderHistoryTable(id, entries)
}

// resolveHistoryPath maps a work-item ID to its file path under hgDir.
// It checks the primary location first, then falls back to archives.
// Returns an error if neither exists.
func resolveHistoryPath(hgDir, id string) (string, error) {
	sub, ext := subDirAndExt(id)
	if sub == "" {
		return "", fmt.Errorf("unknown work-item prefix for %q (expected feat-, bug-, spk-, plan-, or trk-)", id)
	}

	primary := filepath.Join(hgDir, sub, id+ext)
	if _, err := os.Stat(primary); err == nil {
		return primary, nil
	}

	// Fallback: archives directory (flat, may have been renamed).
	archivePath := filepath.Join(hgDir, "archives", id+ext)
	if _, err := os.Stat(archivePath); err == nil {
		return archivePath, nil
	}

	return "", fmt.Errorf("work item %q not found in .wipnote/%s/ or .wipnote/archives/", id, sub)
}

// subDirAndExt returns the subdirectory name and file extension for a given
// work-item ID based on its prefix.
func subDirAndExt(id string) (string, string) {
	switch {
	case strings.HasPrefix(id, "feat-"):
		return "features", ".html"
	case strings.HasPrefix(id, "bug-"):
		return "bugs", ".html"
	case strings.HasPrefix(id, "spk-"):
		return "spikes", ".html"
	case strings.HasPrefix(id, "plan-"):
		return "plans", ".yaml"
	case strings.HasPrefix(id, "trk-"):
		return "tracks", ".html"
	default:
		return "", ""
	}
}

// gitToplevel returns the absolute path to the worktree that owns `dir` by
// invoking `git -C <dir> rev-parse --show-toplevel`. Pinning to `dir` makes
// the lookup independent of process cwd.
func gitToplevel(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git -C %s rev-parse --show-toplevel: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitCommonDir returns the canonical absolute path to the repository's
// shared git dir (the main checkout's .git for linked worktrees). Two
// directories belong to the same repository iff their git-common-dir values
// are equal after symlink resolution — raw strings are not enough because a
// worktree and the .wipnote owner can reach the same repo via different
// symlinked paths (e.g. /tmp vs /private/tmp on macOS, or a dev container
// mount shadowing the host path).
func gitCommonDir(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("git -C %s rev-parse --git-common-dir: %w", dir, err)
	}
	raw := filepath.Clean(strings.TrimSpace(string(out)))
	if resolved, evalErr := filepath.EvalSymlinks(raw); evalErr == nil {
		return resolved, nil
	}
	return raw, nil
}

// resolveHistoryRoot picks the correct git toplevel for `history <id>`.
//
// If the process cwd and the supplied .wipnote owner directory share a
// git-common-dir, they belong to the same repository — cwd may be a linked
// worktree on a different branch, and its toplevel is the right one for
// branch-local history. Otherwise cwd is inside a different repository
// (typically a nested submodule) and we fall back to the .wipnote owner
// so history never escapes the HtmlGraph checkout.
func resolveHistoryRoot(hgOwner string) (string, error) {
	ownerCommon, ownerErr := gitCommonDir(hgOwner)
	cwdCommon, cwdErr := gitCommonDir(".")
	if ownerErr == nil && cwdErr == nil && ownerCommon == cwdCommon {
		if top, err := gitToplevel("."); err == nil {
			return top, nil
		}
	}
	return gitToplevel(hgOwner)
}

// runHistoryLog shells out to git log with --follow to handle renames and
// returns a slice of historyEntry values, newest first.
func runHistoryLog(repoRoot, filePath string) ([]historyEntry, error) {
	// %H = full SHA, %ai = author date ISO 8601, %an = author name, %s = subject
	cmd := exec.Command(
		"git", "log",
		"--follow",
		"--pretty=format:%H\t%ai\t%an\t%s",
		"--",
		filePath,
	)
	cmd.Dir = repoRoot

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	lines := strings.Split(raw, "\n")
	entries := make([]historyEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		entries = append(entries, historyEntry{
			SHA:     parts[0],
			ISOTime: parts[1],
			Author:  parts[2],
			Subject: parts[3],
		})
	}
	return entries, nil
}

// renderHistoryTable pretty-prints entries as aligned columns to stdout.
func renderHistoryTable(id string, entries []historyEntry) error {
	sep := strings.Repeat("─", 72)
	fmt.Println(sep)
	fmt.Printf("  History: %s  (%d commits)\n", id, len(entries))
	fmt.Println(sep)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  DATE\tAUTHOR\tSHA\tSUBJECT")
	for _, e := range entries {
		date := e.ISOTime
		if len(date) >= 19 {
			date = date[:19] // trim timezone
		}
		sha := e.SHA
		if len(sha) > 8 {
			sha = sha[:8]
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
			date,
			truncate(e.Author, 20),
			sha,
			truncate(e.Subject, 50),
		)
	}
	return w.Flush()
}

// renderHistoryJSON marshals entries to indented JSON on stdout.
func renderHistoryJSON(entries []historyEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
