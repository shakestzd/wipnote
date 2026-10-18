package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// claudeTranscriptSession is the newest Claude Code session recorded for a
// project, read from Claude's own transcript files — the same source the
// native /resume picker reads — so the launcher can offer "Continue last
// session" with the label the user would recognise.
type claudeTranscriptSession struct {
	ID      string    // transcript file stem: the id `claude --resume` takes
	Label   string    // custom title, else summary, else first prompt, else last prompt
	Cwd     string    // directory the session ran in; --resume must run there
	ModTime time.Time // last write to the transcript
}

// claudeTranscriptScanLimit bounds how much of a transcript is read for its
// label. Early lines can be large hook attachments, but the first prompt and any
// title or summary line sit near the top.
const claudeTranscriptScanLimit = 8 << 20

// claudeProjectsDir is where Claude Code keeps transcripts: $CLAUDE_CONFIG_DIR
// when set, else ~/.claude, then /projects.
func claudeProjectsDir() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// claudeProjectDirName is Claude Code's folder name for a working directory:
// every character other than an ASCII letter or digit becomes '-'.
func claudeProjectDirName(path string) string {
	b := []byte(path)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// claudeTranscriptCandidates bounds how many transcripts are opened, newest
// first, before giving up: enough to step past a few empty or aborted sessions.
const claudeTranscriptCandidates = 20

// latestClaudeTranscriptSession returns the most recently written Claude session
// for projectRoot or any worktree under it (managed worktrees live in
// <root>/.claude/worktrees, so their folders share the root's prefix). Folder
// names are lossy (a sibling such as /repo/app-copy shares /repo/app's prefix),
// so each candidate's recorded cwd must be the root or a directory under it.
// Candidates are tried newest first, so an empty or aborted newest session does
// not hide an older one. It returns nil when there is none.
func latestClaudeTranscriptSession(projectsDir, projectRoot string) *claudeTranscriptSession {
	if projectsDir == "" || strings.TrimSpace(projectRoot) == "" {
		return nil
	}
	root := filepath.Clean(projectRoot)
	prefix := claudeProjectDirName(root)
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	var candidates []candidate
	for _, d := range dirs {
		if !d.IsDir() || (d.Name() != prefix && !strings.HasPrefix(d.Name(), prefix+"-")) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projectsDir, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.HasPrefix(name, "agent-") {
				continue
			}
			info, err := f.Info()
			if err != nil || info.Size() == 0 {
				continue
			}
			candidates = append(candidates, candidate{filepath.Join(projectsDir, d.Name(), name), info.ModTime()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modTime.After(candidates[j].modTime) })
	if len(candidates) > claudeTranscriptCandidates {
		candidates = candidates[:claudeTranscriptCandidates]
	}
	for _, c := range candidates {
		s := readClaudeTranscriptSession(c.path)
		if s == nil || !withinRoot(s.Cwd, root) {
			continue
		}
		s.ModTime = c.modTime
		return s
	}
	return nil
}

// withinRoot reports whether dir is root or a directory under it. An empty dir
// (no cwd recorded) cannot be placed, so it does not count.
func withinRoot(dir, root string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

// transcriptLine holds the few fields the label needs; everything else in a
// line is skipped by the decoder.
type transcriptLine struct {
	Type        string `json:"type"`
	Cwd         string `json:"cwd"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Summary     string `json:"summary"`
	CustomTitle string `json:"customTitle"`
	LastPrompt  string `json:"lastPrompt"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// readClaudeTranscriptSession reads one transcript's id, cwd and label. It
// returns nil for a transcript with no user prompt at all (an empty or aborted
// session is not worth offering).
func readClaudeTranscriptSession(path string) *claudeTranscriptSession {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	s := &claudeTranscriptSession{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	var title, summary, firstPrompt, lastPrompt string
	dec := json.NewDecoder(io.LimitReader(f, claudeTranscriptScanLimit))
	for {
		var l transcriptLine
		if err := dec.Decode(&l); err != nil {
			break
		}
		if s.Cwd == "" && l.Cwd != "" {
			s.Cwd = l.Cwd
		}
		switch l.Type {
		case "custom-title":
			if t := strings.TrimSpace(l.CustomTitle); t != "" {
				title = t
			}
		case "summary":
			if summary == "" {
				summary = strings.TrimSpace(l.Summary)
			}
		case "last-prompt":
			if p := promptText(l.LastPrompt); p != "" {
				lastPrompt = p
			}
		case "user":
			if firstPrompt == "" && !l.IsMeta && !l.IsSidechain {
				firstPrompt = userPromptText(l.Message.Content)
			}
		}
	}
	if firstPrompt == "" && lastPrompt == "" {
		return nil
	}
	for _, label := range []string{title, summary, firstPrompt, lastPrompt} {
		if label != "" {
			s.Label = label
			break
		}
	}
	return s
}

// userPromptText returns the text a person typed, from a user message's content
// (a string, or an array whose text parts are joined). Tool results and
// harness-injected messages yield "".
func userPromptText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return promptText(str)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "tool_result" {
			return ""
		}
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return promptText(strings.Join(texts, " "))
}

// promptText collapses whitespace and drops harness-injected text: slash-command
// wrappers, task notifications, system reminders and the local-command caveat
// all start with '<' or "Caveat:".
func promptText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || strings.HasPrefix(s, "<") || strings.HasPrefix(s, "Caveat:") {
		return ""
	}
	return s
}
