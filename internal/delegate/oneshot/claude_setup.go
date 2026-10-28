package oneshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// The wrapper's files around an interactive Claude worker (spike
// spk-87139e0e, "What slice 8 must build" 1, 2 and 3).

// ClaudeConfigPath is Claude Code's global config file, where onboarding
// and each folder's workspace trust are kept: $CLAUDE_CONFIG_DIR/.claude.json,
// else ~/.claude.json.
func ClaudeConfigPath() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

// ClaudeDataDir is where Claude Code keeps transcripts:
// $CLAUDE_CONFIG_DIR, else ~/.claude.
func ClaudeDataDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

var projectSlugRe = regexp.MustCompile(`[^A-Za-z0-9]`)

// TranscriptPath is the transcript a session with a known --session-id
// writes: <data>/projects/<work dir, each non-alphanumeric as ->/<sid>.jsonl.
func TranscriptPath(claudeDir, workDir, sid string) string {
	return filepath.Join(claudeDir, "projects", projectSlugRe.ReplaceAllString(workDir, "-"), sid+".jsonl")
}

// workerDir is the folder the worker's claude runs in: wipnote claude runs
// it in the project root, the nearest folder above cwd holding .wipnote/.
func workerDir(cwd string) string {
	for d := cwd; ; d = filepath.Dir(d) {
		if fi, err := os.Stat(filepath.Join(d, ".wipnote")); err == nil && fi.IsDir() {
			return d
		}
		if filepath.Dir(d) == d {
			return cwd
		}
	}
}

// preseedClaude marks onboarding done and each dir's workspace trust
// accepted in the global config, so no dialog blocks the first prompt
// (the spike saw the positional prompt dropped behind them). Every other key
// is kept, and the file is written only when a key was missing, replaced by
// rename. A config that cannot be parsed is left alone.
func preseedClaude(path string, dirs ...string) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	cfg := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("claude config %s: %w; nothing was changed", path, err)
		}
	}
	if !seedKeys(cfg, dirs) {
		return nil
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return filelock.WriteFileAtomic(path, append(out, '\n'), 0o600)
}

// seedKeys sets the missing keys and reports whether any was missing.
func seedKeys(cfg map[string]any, dirs []string) bool {
	changed := false
	if cfg["hasCompletedOnboarding"] != true {
		cfg["hasCompletedOnboarding"], changed = true, true
	}
	projects, _ := cfg["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
	}
	for _, d := range dirs {
		p, _ := projects[d].(map[string]any)
		if p == nil {
			p = map[string]any{}
		}
		if p["hasTrustDialogAccepted"] != true {
			p["hasTrustDialogAccepted"], changed = true, true
		}
		projects[d] = p
	}
	cfg["projects"] = projects
	return changed
}

// writeWorkerSettings writes the worker's --settings file: the caller's own
// settings (inline JSON or a file) with wipnote's status line command, which
// records cost and token totals into the run folder.
func writeWorkerSettings(rf *runstore.RunFolder, own, cwd, statusCmd string) (string, error) {
	cfg := map[string]any{}
	if own != "" {
		data := []byte(own)
		if own[0] != '{' {
			path := own
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			var err error
			if data, err = os.ReadFile(path); err != nil {
				return "", fmt.Errorf("claude --settings %s: %w", own, err)
			}
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return "", fmt.Errorf("claude --settings %s: %w", own, err)
		}
	}
	cfg["statusLine"] = map[string]any{"type": "command", "command": statusCmd, "padding": 0}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := rf.WriteFile(runstore.FileClaudeSettings, append(out, '\n')); err != nil {
		return "", err
	}
	return filepath.Join(rf.Dir(), runstore.FileClaudeSettings), nil
}
