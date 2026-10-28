package oneshot

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// claudePlan is a claude -p argv turned into an interactive worker's launch
// (slice 22): the prompt goes as the brief, the interactive flags as the
// launch args, and the print-only caps are kept for wipnote to enforce.
type claudePlan struct {
	Prompt     string
	Args       []string // interactive flags, in the caller's order
	MaxTurns   int      // 0: no cap
	MaxBudget  float64  // USD; 0: no cap
	SessionID  string   // the caller's own --session-id
	Settings   string   // the caller's own --settings value
	PlanMode   bool
	PluginDirs []string
	Dropped    []string // print-only flags with no interactive meaning
}

// Flags that take one value. A word after one is its value, never a prompt.
var claudeValueFlags = []string{
	"--model", "--effort", "--permission-mode", "--settings", "--setting-sources", "--session-id",
	"--name", "-n", "--append-system-prompt", "--append-system-prompt-file", "--system-prompt",
	"--system-prompt-file", "--agent", "--agents", "--plugin-dir", "--teammate-mode",
	"--fallback-model", "--output-format", "--input-format", "--max-turns", "--max-budget-usd",
	"--json-schema", "--permission-prompt-tool",
}

// Flags that take many values (the spike's launcher gotcha): they read every
// following word that is not a flag. A word with whitespace in it is taken
// as the prompt instead, which is what the caller meant.
var claudeVariadicFlags = []string{
	"--add-dir", "--allowedTools", "--allowed-tools", "--disallowedTools", "--disallowed-tools",
	"--mcp-config", "--betas", "--tools",
}

// Print-only flags (cli-reference: "print mode only"): dropped from the
// interactive launch. --max-turns and --max-budget-usd become wipnote's caps.
var claudePrintOnly = []string{
	"-p", "--print", "--output-format", "--input-format", "--json-schema", "--max-turns",
	"--max-budget-usd", "--no-session-persistence", "--include-partial-messages",
	"--replay-user-messages", "--fallback-model", "--permission-prompt-tool",
}

// claudeResumeFlags continue an old session: such a run keeps the claude -p
// path (a fixed --session-id cannot name a resumed session).
var claudeResumeFlags = []string{"-r", "--resume", "-c", "--continue", "--fork-session"}

// planClaude reads a claude -p argv. An argv it cannot plan returns the
// reason, and the run keeps the claude -p path.
func planClaude(argv []string) (claudePlan, string) {
	var p claudePlan
	var prompts []string
	words := argv[1:]
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "--" {
			prompts = append(prompts, words[i+1:]...)
			break
		}
		if !strings.HasPrefix(w, "-") || w == "-" {
			prompts = append(prompts, w)
			continue
		}
		name, val, inline := strings.Cut(w, "=")
		if slices.Contains(claudeResumeFlags, name) {
			return p, "it resumes or continues a session"
		}
		vals, next := flagValues(name, words, i, inline, val)
		if err := p.take(name, vals); err != "" {
			return p, err
		}
		if !slices.Contains(claudePrintOnly, name) {
			p.Args = append(p.Args, words[i:next+1]...)
		} else if name != "-p" && name != "--print" && name != "--max-turns" && name != "--max-budget-usd" {
			p.Dropped = append(p.Dropped, name)
		}
		i = next
	}
	if len(prompts) != 1 || strings.TrimSpace(prompts[0]) == "" {
		return p, fmt.Sprintf("it has %d prompt words, not one quoted prompt", len(prompts))
	}
	p.Prompt = prompts[0]
	return p, ""
}

// flagValues returns the values of the flag at words[i] and the index of
// its last word.
func flagValues(name string, words []string, i int, inline bool, val string) ([]string, int) {
	switch {
	case inline:
		return []string{val}, i
	case slices.Contains(claudeValueFlags, name):
		if i+1 < len(words) {
			return words[i+1 : i+2], i + 1
		}
		return nil, i
	case slices.Contains(claudeVariadicFlags, name):
		j := i + 1
		for j < len(words) && !strings.HasPrefix(words[j], "-") && !strings.ContainsAny(words[j], " \t\n") {
			j++
		}
		return words[i+1 : j], j - 1
	}
	return nil, i // a boolean flag, or one this list does not know
}

// take keeps what the plan needs from one flag.
func (p *claudePlan) take(name string, vals []string) string {
	v := ""
	if len(vals) > 0 {
		v = vals[0]
	}
	var err error
	switch name {
	case "--max-turns":
		p.MaxTurns, err = strconv.Atoi(v)
	case "--max-budget-usd":
		p.MaxBudget, err = strconv.ParseFloat(v, 64)
	case "--session-id":
		p.SessionID = v
	case "--settings":
		p.Settings = v
	case "--permission-mode":
		p.PlanMode = v == "plan"
	case "--plugin-dir":
		p.PluginDirs = append(p.PluginDirs, v)
	}
	if err != nil {
		return fmt.Sprintf("%s %q is not a number", name, v)
	}
	return ""
}

// missingPluginDir is the first --plugin-dir that is not a directory:
// claude starts without it and says nothing (spike), so the run refuses.
func (p claudePlan) missingPluginDir(cwd string) string {
	for _, d := range p.PluginDirs {
		path := d
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			return d
		}
	}
	return ""
}
