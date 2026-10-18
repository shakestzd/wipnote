package hooks

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Codex has no Read tool: it inspects files through its shell tool, which
// reaches the hook as "Bash". isShortShellRead gives those reads the same
// short-read allowance as Read (Codex review on #209). It recognises only one
// plain command — cat, head, tail, or `sed -n 'A,Bp'` — on one file. Anything
// that could chain, redirect, expand or glob is refused, so the guard fails
// closed on every command it does not fully understand.

// shellReadUnsafeChars are refused anywhere in the command, even inside
// quotes: chaining, pipes, redirects, substitution, globs and escapes.
const shellReadUnsafeChars = "|;&<>`$(){}*?[]\\\n"

// sedPrintRange matches a sed -n script that only prints lines A..B (or A).
var sedPrintRange = regexp.MustCompile(`^([0-9]+)(?:,([0-9]+))?p$`)

// isShortShellRead reports whether cmd is a single read of at most
// orchestratorShortReadLines lines. Relative paths resolve against baseDir.
func isShortShellRead(cmd, baseDir string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, shellReadUnsafeChars) {
		return false
	}
	words := tokenizeShellWords(cmd)
	if len(words) < 2 {
		return false
	}
	switch words[0] {
	case "cat":
		return shellReadCat(words[1:], baseDir)
	case "head", "tail":
		return shellReadHeadTail(words[1:], baseDir)
	case "sed":
		return shellReadSed(words[1:], baseDir)
	}
	return false
}

// shellReadCat accepts `cat [-n] FILE` when FILE is short.
func shellReadCat(args []string, baseDir string) bool {
	if len(args) == 2 && args[0] == "-n" {
		args = args[1:]
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return false
	}
	return fileHasAtMostLines(resolveReadPath(args[0], baseDir), orchestratorShortReadLines)
}

// shellReadHeadTail accepts `head|tail [-n N | -nN | -N] FILE`. A count in
// range is short whatever the file; no count means the default 10 lines; a
// `+N` start reads to the end, so the file itself must be short.
func shellReadHeadTail(args []string, baseDir string) bool {
	count := ""
	switch {
	case len(args) == 3 && args[0] == "-n":
		count, args = args[1], args[2:]
	case len(args) == 2 && strings.HasPrefix(args[0], "-n"):
		count, args = strings.TrimPrefix(args[0], "-n"), args[1:]
	case len(args) == 2 && strings.HasPrefix(args[0], "-"):
		count, args = strings.TrimPrefix(args[0], "-"), args[1:]
	case len(args) == 1:
		count = "10"
	default:
		return false
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return false
	}
	path := resolveReadPath(args[0], baseDir)
	if strings.HasPrefix(count, "+") {
		return fileHasAtMostLines(path, orchestratorShortReadLines)
	}
	n, err := strconv.Atoi(count)
	if err != nil || n <= 0 {
		return false
	}
	return n <= orchestratorShortReadLines || fileHasAtMostLines(path, orchestratorShortReadLines)
}

// shellReadSed accepts `sed -n 'A,Bp' FILE` printing at most the limit, or
// any print range on a short file.
func shellReadSed(args []string, baseDir string) bool {
	if len(args) != 3 || args[0] != "-n" || strings.HasPrefix(args[2], "-") {
		return false
	}
	m := sedPrintRange.FindStringSubmatch(args[1])
	if m == nil {
		return false
	}
	from, _ := strconv.Atoi(m[1])
	to := from
	if m[2] != "" {
		to, _ = strconv.Atoi(m[2])
	}
	if from >= 1 && to >= from && to-from+1 <= orchestratorShortReadLines {
		return true
	}
	return fileHasAtMostLines(resolveReadPath(args[2], baseDir), orchestratorShortReadLines)
}

// resolveReadPath joins a relative path onto baseDir.
func resolveReadPath(path, baseDir string) string {
	if !filepath.IsAbs(path) && baseDir != "" {
		return filepath.Join(baseDir, path)
	}
	return path
}
