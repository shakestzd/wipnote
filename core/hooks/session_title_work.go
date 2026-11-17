package hooks

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	titleGitTimeout   = 5 * time.Second
	titleMaxSubjects  = 10
	titleMaxFiles     = 30
	titleGitLogWindow = "-n10"
)

// sessionWork reads the commits made in repoDir since since and the files
// they and the working tree changed. A repo git cannot read gives no work.
func sessionWork(repoDir string, since time.Time) sessionWorkSummary {
	if repoDir == "" {
		return sessionWorkSummary{}
	}
	var w sessionWorkSummary
	if !since.IsZero() {
		after := "--since=@" + strconv.FormatInt(since.Unix(), 10)
		w.Subjects = titleGitLines(repoDir, "log", titleGitLogWindow, after, "--format=%s")
		w.Files = titleGitLines(repoDir, "log", titleGitLogWindow, after, "--name-only", "--format=")
	}
	for _, line := range titleGitLines(repoDir, "status", "--porcelain", "--untracked-files=normal") {
		if len(line) > 3 {
			path := line[3:]
			if i := strings.Index(path, " -> "); i >= 0 {
				path = path[i+4:]
			}
			w.Files = append(w.Files, strings.Trim(path, `"`))
		}
	}
	w.Files = slices.DeleteFunc(w.Files, func(f string) bool { return strings.HasPrefix(f, ".wipnote/") })
	slices.Sort(w.Files)
	w.Files = slices.Compact(w.Files)
	if len(w.Subjects) > titleMaxSubjects {
		w.Subjects = w.Subjects[:titleMaxSubjects]
	}
	if len(w.Files) > titleMaxFiles {
		w.Files = w.Files[:titleMaxFiles]
	}
	return w
}

// titleGitLines runs git in dir and returns its non-empty output lines, or
// nil on any failure.
func titleGitLines(dir string, args ...string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), titleGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var lines []string
	for l := range strings.SplitSeq(string(out), "\n") {
		if l = strings.TrimRight(l, "\r"); strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
