package hooks

import (
	"regexp"
	"strings"
)

// Store guard: decides whether a shell command MUTATES the managed .wipnote/
// store (GH-#180). The decision is made per shell segment on resolved operation
// targets — redirect destinations, arguments of file-mutating commands, and the
// pathspecs of path-taking git subcommands — never on the store path merely
// appearing somewhere in the command text. Heredoc bodies are dropped and
// segments are never split inside a quoted span before segmenting, so quoted
// prose ("never run git add against .wipnote/", or "Never; rm .wipnote/x")
// is never a target and documenting the rule cannot trip the guard. Runtime
// scratch directories the store never commits (.wipnote/logs/) are exempt —
// see isWipnoteRuntimeExemptPath.

// bashCommandWritesWipnoteStore reports whether any segment of cmd mutates the
// store. Segments that invoke the wipnote CLI itself are exempt: the CLI is the
// sanctioned writer (and commitWipnoteArtifact runs inside that process, so it
// never passes through this hook at all).
func bashCommandWritesWipnoteStore(cmd string) bool {
	for _, segment := range splitShellCommandSegments(stripHeredocBodies(cmd)) {
		if segmentStartsWithWipnoteCLI(segment) {
			continue
		}
		if segmentMutatesWipnoteStore(segment) {
			return true
		}
	}
	return false
}

// heredocStartRe matches a heredoc operator (<<EOF, <<-EOF, <<'EOF', <<"EOF",
// <<\EOF). The leading (?:^|[^<]) keeps here-strings (<<<) from matching.
var heredocStartRe = regexp.MustCompile(`(?:^|[^<])<<-?\s*(?:'([^']+)'|"([^"]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

// stripHeredocBodies removes the body lines of every heredoc in cmd, keeping
// the line that carries the operator (its redirects are still targets).
func stripHeredocBodies(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	lines := strings.Split(cmd, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		m := heredocStartRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		delim := m[1] + m[2] + m[3]
		for i+1 < len(lines) {
			i++
			if strings.TrimSpace(lines[i]) == delim {
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

// segmentMutatesWipnoteStore resolves the operation targets of one shell
// segment and reports whether any of them is inside the store.
func segmentMutatesWipnoteStore(segment string) bool {
	words := tokenizeShellWords(segment)
	if redirectTargetsWipnoteStore(words) {
		return true
	}
	cmd, args := splitShellCommandArgs(words)
	switch cmd {
	case "rm", "mv", "cp", "touch", "chmod", "mkdir", "tee":
		return anyWipnoteStorePath(args)
	case "sed":
		return hasSedInPlaceFlag(args) && anyWipnoteStorePath(args)
	case "python", "python2", "python3":
		return pythonInlineCodeTouchesStore(args)
	case "git":
		return gitMutatesWipnoteStore(args)
	}
	return false
}

// redirectTargetsWipnoteStore reports whether any output redirect (>, >>, N>,
// N>>, &>, &>>) in words points into the store.
func redirectTargetsWipnoteStore(words []string) bool {
	for i, w := range words {
		if !isOutputRedirectOp(w) || i+1 >= len(words) {
			continue
		}
		if isWipnoteStorePath(words[i+1]) {
			return true
		}
	}
	return false
}

// splitShellCommandArgs returns the command word and its arguments, skipping
// leading env assignments, redirect operators with their targets, and leading
// subshell/group punctuation.
func splitShellCommandArgs(words []string) (string, []string) {
	var plain []string
	for i := 0; i < len(words); i++ {
		w := words[i]
		if isOutputRedirectOp(w) {
			i++ // skip the redirect target
			continue
		}
		plain = append(plain, w)
	}
	for len(plain) > 0 {
		first := strings.TrimLeft(plain[0], "({ ")
		if first == "" {
			plain = plain[1:]
			continue
		}
		if strings.Contains(first, "=") && !strings.HasPrefix(first, "-") {
			plain = plain[1:]
			continue
		}
		return first, plain[1:]
	}
	return "", nil
}

// isOutputRedirectOp reports whether a token is a stdout-style redirect
// operator produced by tokenizeShellWords: >, >>, 1>, 2>>, &>, &>>.
func isOutputRedirectOp(tok string) bool {
	trimmed := strings.TrimRight(tok, ">")
	if len(trimmed) == len(tok) {
		return false
	}
	if trimmed == "" || trimmed == "&" {
		return true
	}
	return strings.Trim(trimmed, "0123456789") == ""
}

// isWipnoteStorePath reports whether a single argument is a path inside the
// managed store: ".wipnote", ".wipnote/…", or ".wipnote/" as a path element
// after "/" or "=". Prose containing whitespace, or ".wipnote/" embedded in a
// sed/regex expression (e.g. 's|.wipnote/|x|'), is not a target.
func isWipnoteStorePath(arg string) bool {
	arg = strings.TrimRight(arg, ")};")
	if arg == "" || strings.ContainsAny(arg, " \t\n") {
		return false
	}
	if arg == ".wipnote" || strings.HasSuffix(arg, "/.wipnote") {
		return true // the store root itself, not a runtime subpath under it
	}
	if rel, ok := strings.CutPrefix(arg, ".wipnote/"); ok {
		return !isWipnoteRuntimeExemptPath(rel)
	}
	for idx := strings.Index(arg, ".wipnote/"); idx > 0; {
		if prev := arg[idx-1]; prev == '/' || prev == '=' {
			return !isWipnoteRuntimeExemptPath(arg[idx+len(".wipnote/"):])
		}
		next := strings.Index(arg[idx+1:], ".wipnote/")
		if next < 0 {
			break
		}
		idx += 1 + next
	}
	return false
}

// wipnoteRuntimeExemptPrefixes are .wipnote/ subdirectories that are
// gitignored runtime scratch space (see .wipnote/.gitignore's "Runtime/
// session directories" section) rather than committed store content — the
// guard's job is protecting the canonical, git-tracked work-item artifacts,
// and a plain Bash write under one of these can never mutate anything that
// exists to protect. "logs/" is the concrete case: the context-pack Act-First
// preamble (feat-a4f1332a) mandates `mkdir`/`>>` into .wipnote/logs/progress/
// as an agent's literal first tool call, before `wipnote start` has even run.
var wipnoteRuntimeExemptPrefixes = []string{"logs/"}

func isWipnoteRuntimeExemptPath(rel string) bool {
	for _, prefix := range wipnoteRuntimeExemptPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

func anyWipnoteStorePath(args []string) bool {
	for _, a := range args {
		if isWipnoteStorePath(a) {
			return true
		}
	}
	return false
}

func hasSedInPlaceFlag(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-i") || strings.HasPrefix(a, "--in-place") {
			return true
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "i") {
			return true // combined short flags such as -ni / -Ei
		}
	}
	return false
}

// pythonInlineCodeTouchesStore reports whether a `python -c <code>` literal
// references the store. Inline code is executable, not prose, so a mention in
// it is treated as a target.
func pythonInlineCodeTouchesStore(args []string) bool {
	for i, a := range args {
		if a == "-c" && i+1 < len(args) {
			return strings.Contains(args[i+1], ".wipnote/")
		}
	}
	return false
}

// gitMutatesWipnoteStore reports whether a git invocation runs a path-taking
// mutating subcommand (add, rm, mv, restore, checkout, stash [push]) with a
// pathspec inside the store. Global options before the subcommand (-C dir,
// -c k=v, --no-pager, …) are skipped.
func gitMutatesWipnoteStore(args []string) bool {
	sub, rest := gitSubcommand(args)
	switch sub {
	case "add", "rm", "mv", "restore", "checkout":
		return anyWipnoteStorePath(rest)
	case "stash":
		if len(rest) > 0 && rest[0] == "push" {
			rest = rest[1:]
		}
		return anyWipnoteStorePath(rest)
	}
	return false
}

func gitSubcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a, args[i+1:]
		}
		if a == "-C" || a == "-c" {
			i++ // option takes a separate value
		}
	}
	return "", nil
}

// tokenizeShellWords splits one shell segment into words with POSIX-style
// quote handling ('…', "…", backslash). Quotes are removed from the returned
// words. Output redirect operators (>, >>, N>, N>>, &>, &>>) are emitted as
// their own words so callers can pair them with their target.
func tokenizeShellWords(segment string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		switch {
		case c == ' ' || c == '\t':
			flush()
		case c == '\\' && i+1 < len(segment):
			i++
			cur.WriteByte(segment[i])
		case c == '\'':
			end := strings.IndexByte(segment[i+1:], '\'')
			if end < 0 {
				end = len(segment) - i - 1
			}
			cur.WriteString(segment[i+1 : i+1+end])
			i += end + 1
		case c == '"':
			i = readDoubleQuoted(segment, i+1, &cur)
		case c == '&' && i+1 < len(segment) && segment[i+1] == '>':
			flush()
			cur.WriteByte('&')
		case c == '>':
			if s := cur.String(); s != "&" && strings.Trim(s, "0123456789") != "" {
				flush()
			}
			cur.WriteByte('>')
			if i+1 < len(segment) && segment[i+1] == '>' {
				cur.WriteByte('>')
				i++
			}
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return words
}

// readDoubleQuoted appends the body of a double-quoted string starting at
// segment[start] to cur and returns the index of the closing quote (or the
// last index when unterminated).
func readDoubleQuoted(segment string, start int, cur *strings.Builder) int {
	i := start
	for ; i < len(segment); i++ {
		c := segment[i]
		if c == '\\' && i+1 < len(segment) && strings.IndexByte(`"\$`+"`", segment[i+1]) >= 0 {
			i++
			cur.WriteByte(segment[i])
			continue
		}
		if c == '"' {
			return i
		}
		cur.WriteByte(c)
	}
	return i
}
