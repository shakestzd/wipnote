package codex

import (
	"slices"
	"strings"
)

// ownWords are the tool's flags up to a "--", after which all is prompt.
func ownWords(args []string) []string {
	if i := slices.Index(args, "--"); i >= 0 {
		return args[:i]
	}
	return args
}

// sandboxWord reports whether words[i] sets the sandbox, its value, and
// how many words it spans (2 for "--sandbox V" and "-s V").
func sandboxWord(words []string, i int) (value string, span int, ok bool) {
	w := words[i]
	switch {
	case w == "--sandbox" || w == "-s":
		if i+1 < len(words) {
			return words[i+1], 2, true
		}
		return "", 1, true
	case strings.HasPrefix(w, "--sandbox="):
		return strings.TrimPrefix(w, "--sandbox="), 1, true
	case strings.HasPrefix(w, "-s") && !strings.HasPrefix(w, "--"):
		v := strings.TrimPrefix(strings.TrimPrefix(w, "-s"), "=")
		if _, valid := ParseMode(v); valid {
			return v, 1, true
		}
	}
	return "", 0, false
}

// sandboxValue is the first sandbox the words set, in each spelling clap
// accepts (--sandbox V, --sandbox=V, -s V, -s=V, -sV).
func sandboxValue(words []string) (string, bool) {
	for i := range words {
		if v, _, ok := sandboxWord(words, i); ok {
			return v, true
		}
	}
	return "", false
}

// RequestedSandbox is the --sandbox (or -s) value in args, or "" when none
// is given. The launcher skips its devcontainer fallback when it is set.
func RequestedSandbox(args []string) string {
	v, _ := sandboxValue(ownWords(args))
	return v
}

// BypassesSandbox reports whether args turn the sandbox off entirely.
func BypassesSandbox(args []string) bool {
	return slices.Contains(ownWords(args), bypassFlag)
}

// WithSandbox returns args with the caller's own sandbox words (--sandbox,
// -s, --full-auto and the bypass flag) replaced by one --sandbox m in
// front. Words after "--" are prompt and are never touched.
func WithSandbox(args []string, m Mode) []string {
	own := ownWords(args)
	out := []string{"--sandbox", string(m)}
	for i := 0; i < len(own); i++ {
		if _, span, ok := sandboxWord(own, i); ok {
			i += span - 1
			continue
		}
		if own[i] == "--full-auto" || own[i] == bypassFlag {
			continue
		}
		out = append(out, own[i])
	}
	return append(out, args[len(own):]...)
}
