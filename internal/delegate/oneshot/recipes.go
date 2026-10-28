// Package oneshot is `wipnote delegate run` (plan-28b4a7f6 slice 8): one
// delegated tool call (codex exec or review, claude -p, ccx task) run in a
// herdr pane when the caller is inside herdr, else headless, with the
// codex-herdr-pane mod's run folder and report.
//
// The mod generated a shell wrapper and a pane script for every call. Here Go
// runs the tool's argv directly: there is no generated script, no shim and no
// shell between the caller and the tool. Chained commands (pipes, ;) are
// refused in v1 (q-chained).
package oneshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

// Kind is the delegated tool. Its value is the ledger's harness column.
type Kind string

const (
	KindCodex       Kind = "codex"        // codex exec (or e)
	KindCodexReview Kind = "codex-review" // codex review
	KindClaude      Kind = "claude-print" // claude -p / --print
	KindCcx         Kind = "ccx"          // ccx task
)

// Recipe is a classified tool argv, as given by the caller.
type Recipe struct {
	Kind Kind
	Argv []string
}

// ErrChained refuses shell syntax: v1 runs one tool argv, not a command line.
var ErrChained = errors.New("delegate run: chained commands (pipes, ;, redirects) are not supported in v1; pass one tool argv after --")

// shellWords are argv words that only mean something to a shell. Arriving
// as argv they were quoted on purpose or meant for a shell that never ran.
var shellWords = []string{"|", "||", "&", "&&", ";", ";;", ">", ">>", "<", "<<", "2>", "2>&1", "&>", "1>", "|&"}

// claudeSubcommands name claude subcommands, never a print run (the mod's
// CLAUDE_SUBS).
var claudeSubcommands = strings.Split("mcp|plugin|plugins|auth|agents|attach|auto-mode|daemon|doctor|gateway|import|install|logs|purge|remote-control|respawn|rm|self-hosted-runner|setup-token|stop|kill|ultrareview|update", "|")

var infoFlags = []string{"--help", "-h", "--version", "-V"}

// Classify names the tool of argv, or refuses it with the reason.
func Classify(argv []string) (Recipe, error) {
	if len(argv) == 0 {
		return Recipe{}, errors.New("delegate run: no command after --")
	}
	if err := refuseShell(argv); err != nil {
		return Recipe{}, err
	}
	if words := ownWords(argv[1:]); slices.ContainsFunc(words, func(w string) bool { return slices.Contains(infoFlags, w) }) {
		return Recipe{}, fmt.Errorf("delegate run: %s asks for help or a version, which is not a run; call it directly", strings.Join(argv, " "))
	}
	r := Recipe{Argv: slices.Clone(argv)}
	sub := ""
	if len(argv) > 1 {
		sub = argv[1]
	}
	switch filepath.Base(argv[0]) {
	case "codex":
		switch sub {
		case "exec", "e":
			r.Kind = KindCodex
		case "review":
			r.Kind = KindCodexReview
		}
	case "ccx":
		if sub == "task" {
			r.Kind = KindCcx
		}
	case "claude":
		r.Kind = claudeKind(argv[1:])
	}
	if r.Kind == "" {
		return Recipe{}, fmt.Errorf("delegate run: %q is not codex exec, codex review, claude -p or ccx task", strings.Join(argv, " "))
	}
	if err := r.refuseOwnFormat(); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

func refuseShell(argv []string) error {
	if len(argv) == 1 && strings.ContainsAny(argv[0], " \t\n") {
		return fmt.Errorf("%w (got one shell string %q)", ErrChained, argv[0])
	}
	for _, w := range argv {
		if slices.Contains(shellWords, w) {
			return fmt.Errorf("%w (got %q)", ErrChained, w)
		}
	}
	return nil
}

// ownWords are the tool's words up to a "--", after which all is prompt.
func ownWords(args []string) []string {
	if i := slices.Index(args, "--"); i >= 0 {
		return args[:i]
	}
	return args
}

func claudeKind(args []string) Kind {
	if len(args) > 0 && slices.Contains(claudeSubcommands, args[0]) {
		return ""
	}
	words := ownWords(args)
	if slices.Contains(words, "-p") || slices.Contains(words, "--print") {
		return KindClaude
	}
	return ""
}

// refuseOwnFormat: the run reads claude's stream-json, so a caller's other
// --output-format would leave the report with nothing to read.
func (r Recipe) refuseOwnFormat() error {
	if r.Kind != KindClaude {
		return nil
	}
	if f, ok := flagValue(ownWords(r.Argv[1:]), "--output-format"); ok && f != "stream-json" {
		return fmt.Errorf("delegate run: claude --output-format %s is not supported; delegate run reads stream-json itself, so drop the flag", f)
	}
	return nil
}

// flagValue finds --name V or --name=V.
func flagValue(words []string, name string) (string, bool) {
	for i, w := range words {
		if w == name && i+1 < len(words) {
			return words[i+1], true
		}
		if v, ok := strings.CutPrefix(w, name+"="); ok {
			return v, true
		}
	}
	return "", false
}

// Name is the tool's name in the report's status line.
func (r Recipe) Name() string {
	switch r.Kind {
	case KindClaude:
		return "claude"
	case KindCcx:
		return "ccx"
	}
	return "codex"
}

// CommandOptions shape the argv the run executes.
type CommandOptions struct {
	LastFile    string     // the run folder's last.md
	LeanProfile string     // empty: off
	CodexHome   string     // where codex reads <profile>.config.toml
	Sandbox     codex.Mode // codex exec's --sandbox (slice 12); empty: as given
}

// Command is the argv the run executes. codex exec gets
// --output-last-message, --json and the lean profile, each only when the
// caller did not say so, and the ladder's one --sandbox; claude gets --output-format stream-json and
// --verbose (print mode refuses stream-json without it). review and ccx run
// as given.
func (r Recipe) Command(o CommandOptions) []string {
	switch r.Kind {
	case KindCodex:
		words := ownWords(r.Argv[2:])
		var pre []string
		if _, own := r.OwnLastFile(); !own {
			pre = append(pre, "--output-last-message", o.LastFile)
		}
		if p := o.LeanProfile; p != "" && !hasProfile(words) && isFile(filepath.Join(o.CodexHome, p+".config.toml")) {
			pre = append(pre, "-p", p)
		}
		if !slices.Contains(words, "--json") {
			pre = append(pre, "--json")
		}
		rest := r.Argv[2:]
		if o.Sandbox != "" {
			rest = codex.WithSandbox(rest, o.Sandbox) // exactly one --sandbox
		}
		return slices.Concat(r.Argv[:2], pre, rest)
	case KindClaude:
		pre := []string{}
		if _, ok := flagValue(ownWords(r.Argv[1:]), "--output-format"); !ok {
			pre = append(pre, "--output-format", "stream-json")
		}
		if !slices.Contains(ownWords(r.Argv[1:]), "--verbose") {
			pre = append(pre, "--verbose")
		}
		return slices.Concat(r.Argv[:1], pre, r.Argv[1:])
	}
	return slices.Clone(r.Argv)
}

func hasProfile(words []string) bool {
	return slices.ContainsFunc(words, func(w string) bool {
		return w == "-p" || w == "--profile" || strings.HasPrefix(w, "--profile=")
	})
}

// OwnLastFile is the caller's own codex exec -o file, in each spelling clap
// accepts (-o F, -oF, -o=F, --output-last-message[=]F).
func (r Recipe) OwnLastFile() (string, bool) {
	if r.Kind != KindCodex {
		return "", false
	}
	words := ownWords(r.Argv[2:])
	for i, w := range words {
		switch {
		case w == "-o" || w == "--output-last-message":
			if i+1 < len(words) {
				return words[i+1], true
			}
			return "", true
		case strings.HasPrefix(w, "--output-last-message="):
			return strings.TrimPrefix(w, "--output-last-message="), true
		case strings.HasPrefix(w, "-o") && len(w) > 2:
			return strings.TrimPrefix(strings.TrimPrefix(w, "-o"), "="), true
		}
	}
	return "", false
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// labelWord keeps the characters the mod's pane titles keep.
var labelWord = regexp.MustCompile(`[^A-Za-z0-9 _.:/=+-]`)

// DerivedLabel is the pane title the mod derives (tools.ts paneLabel): a
// short tool title, then the start of the prompt, or what a review looks at.
// It goes only to the run folder's label file, never to the ledger.
func (r Recipe) DerivedLabel() string {
	text := r.promptText()
	title := map[Kind]string{KindCodex: "cdx", KindCodexReview: "cdx review", KindCcx: "ccx task", KindClaude: "Claude task"}[r.Kind]
	if r.Kind == KindClaude && strings.HasPrefix(text, "/code-review") {
		title = "Claude review"
	}
	if r.Kind == KindCodexReview && text == "" {
		text = reviewTarget(r.Argv[2:])
	}
	if text == "" {
		text = "running"
	}
	if rs := []rune(text); len(rs) > 48 {
		text = string(rs[:48])
	}
	return labelWord.ReplaceAllString(title+": "+strings.TrimSpace(text), "")
}

// promptText is the first argument with a space in it (a prompt), else the
// last word that is not a flag; collapsed to one line.
func (r Recipe) promptText() string {
	start := min(2, len(r.Argv))
	if r.Kind == KindClaude {
		start = 1
	}
	args := r.Argv[start:]
	for _, a := range args {
		if strings.ContainsAny(strings.TrimSpace(a), " \t\n") {
			return strings.Join(strings.Fields(a), " ")
		}
	}
	if n := len(args); n > 0 && !strings.HasPrefix(args[n-1], "-") && (n == 1 || !strings.HasPrefix(args[n-2], "-")) {
		return args[n-1]
	}
	return ""
}

// reviewTarget is what a review with no prompt looks at, from its flags.
func reviewTarget(args []string) string {
	if b, ok := flagValue(args, "--base"); ok {
		return "changes against " + b
	}
	if c, ok := flagValue(args, "--commit"); ok {
		return "commit " + c[:min(12, len(c))]
	}
	return "current changes"
}
