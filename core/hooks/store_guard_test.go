package hooks

import (
	"reflect"
	"testing"
)

// TestBashCommandWritesWipnoteStore pins the per-segment, target-based store
// guard (GH-#180): mutations whose TARGET is inside .wipnote/ are blocked,
// commands that merely mention the path (heredoc bodies, quoted prose, sed
// expressions, unrelated arguments) are allowed.
func TestBashCommandWritesWipnoteStore(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		// --- blocked: direct file mutations targeting the store ---
		{"rm file", "rm -f .wipnote/features/feat-x.html", true},
		{"rm quoted target", `rm -rf ".wipnote/features/feat-x.html"`, true},
		{"rm whole dir", "rm -rf .wipnote", true},
		{"rm abs path", "rm /home/u/proj/.wipnote/bugs/bug-1.html", true},
		{"rm env-expanded path", "rm $ROOT/.wipnote/bugs/bug-1.html", true},
		{"redirect", "echo wipnote > .wipnote/features/feat-abc.html", true},
		{"redirect no space", "echo x >.wipnote/features/feat-abc.html", true},
		{"append redirect", "echo x >> .wipnote/features/feat-abc.html", true},
		{"fd redirect", "cmd 2>.wipnote/debug.log", true},
		{"combined redirect", "cmd &> .wipnote/debug.log", true},
		{"heredoc with store redirect", "cat > .wipnote/x.html <<'EOF'\n<html>\nEOF", true},
		{"tee", "echo x | tee .wipnote/features/feat-abc.html", true},
		{"mv into store", "mv feat.html .wipnote/features/feat.html", true},
		{"cp into store", "cp -r backup/ .wipnote/", true},
		{"touch", "touch .wipnote/features/feat-abc.html", true},
		{"chmod", "chmod 644 .wipnote/features/feat-abc.html", true},
		{"mkdir", "mkdir -p .wipnote/features", true},
		{"sed in place", "sed -i 's/todo/done/' .wipnote/features/feat-abc.html", true},
		{"sed combined flags", "sed -Ei 's/todo/done/' .wipnote/features/feat-abc.html", true},
		{"python -c", `python3 -c "open('.wipnote/features/f.html','w').write('x')"`, true},
		{"cd then rm", "cd proj && rm .wipnote/features/feat-abc.html", true},
		{"subshell", "(rm -rf .wipnote/features/feat-abc.html)", true},
		{"mixed wipnote CLI and direct write", "wipnote status && echo x > .wipnote/features/feat-abc.html", true},

		// --- blocked: path-taking git subcommands against the store ---
		{"git add dir", "git add .wipnote/ && git commit -m 'x'", true},
		{"git add bare dir", "git add .wipnote", true},
		{"git add file", "git add .wipnote/features/feat-abc.html", true},
		{"git add with global opts", "git -C /repo --no-pager add .wipnote/", true},
		{"git rm", "git rm --cached .wipnote/features/feat-abc.html", true},
		{"git mv", "git mv .wipnote/features/a.html .wipnote/features/b.html", true},
		{"git restore", "git restore --staged .wipnote/", true},
		{"git checkout double dash", "git checkout -- .wipnote/features/feat-abc.html", true},
		{"git checkout ref path", "git checkout HEAD -- .wipnote/", true},
		{"git stash push", "git stash push -m wip -- .wipnote/", true},
		{"git stash implicit push", "git stash -- .wipnote/features/feat-abc.html", true},

		// --- allowed: the wipnote CLI is the sanctioned writer ---
		{"wipnote CLI", "wipnote feature start feat-abc", false},
		{"wipnote CLI with env prefix", "WIPNOTE_AGENT_ID=x wipnote bug complete bug-1 >/dev/null 2>&1", false},
		{"wipnote CLI path", "/usr/local/bin/wipnote feature complete feat-abc", false},

		// --- allowed: mentions that are not operation targets ---
		{"heredoc mention outside store", "cat > ~/notes.md <<'TXT'\nNever run git add against .wipnote/ by hand.\nTXT", false},
		{"heredoc mention with rm", "cat <<EOF > docs/rules.md\nDo not rm -rf .wipnote/ or cp into it.\nEOF", false},
		{"quoted prose argument", `echo "never run git add against .wipnote/ by hand" > notes.md`, false},
		{"git commit message mention", `git commit -m "fix: stop staging .wipnote/ directly"`, false},
		{"sed expression mention", "sed -i 's|.wipnote/|store/|' README.md", false},
		{"grep in store", "grep -r todo .wipnote/features/", false},
		{"cat store file", "cat .wipnote/features/feat-abc.html", false},
		{"ls store", "ls -la .wipnote/", false},
		{"git status store", "git status .wipnote/", false},
		{"git diff store", "git diff -- .wipnote/", false},
		{"git log store", "git log --oneline -- .wipnote/", false},
		{"git add elsewhere", "git add core/hooks/store_guard.go", false},
		{"rm elsewhere", "rm -rf build/.wipnote-cache", false},
		{"redirect elsewhere", "echo x > wipnote.log", false},
		{"stderr to devnull", "wipnote-ish-tool 2>/dev/null", false},
		{"empty", "", false},

		// --- allowed: a semicolon inside a quoted argument is not a segment
		// boundary, so the "rm .wipnote/x" fragment is never split out as
		// its own segment and misclassified as a real rm target ---
		{"quoted semicolon not a boundary", `printf '%s\n' "Never; rm .wipnote/x" > docs/rules.md`, false},
		{"quoted pipe not a boundary", `echo "a | rm .wipnote/x" > docs/rules.md`, false},
		{"quoted && not a boundary", `echo "safe && rm .wipnote/x" > docs/rules.md`, false},

		// --- allowed: .wipnote/logs/ is gitignored runtime scratch space,
		// never part of the committed store, so the context-pack Act-First
		// preamble's mandated mkdir/append can run under a Bash guard ---
		{"mkdir logs progress dir", "mkdir -p .wipnote/logs/progress", false},
		{"append to logs progress note", "printf 'started\\n' >> .wipnote/logs/progress/feat-abc.md", false},

		// --- blocked: logs/ is exempt, but the rest of the store is not ---
		{"redirect into features despite logs exemption elsewhere", "echo x > .wipnote/features/feat-abc.html", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := bashCommandWritesWipnoteStore(tc.cmd); got != tc.want {
				t.Errorf("bashCommandWritesWipnoteStore(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestStripHeredocBodies(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no heredoc", "echo a\necho b", "echo a\necho b"},
		{"quoted delimiter", "cat > f <<'EOF'\nline1\nline2\nEOF\necho done", "cat > f <<'EOF'\necho done"},
		{"bare delimiter", "cat <<EOF\nbody\nEOF", "cat <<EOF"},
		{"dash delimiter with tabs", "cat <<-EOF\n\tbody\n\tEOF\necho after", "cat <<-EOF\necho after"},
		{"unterminated drops rest", "cat <<EOF\nbody\nmore", "cat <<EOF"},
		{"here-string untouched", "cat <<<'.wipnote/x'\necho y", "cat <<<'.wipnote/x'\necho y"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripHeredocBodies(tc.in); got != tc.want {
				t.Errorf("stripHeredocBodies(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTokenizeShellWords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "rm -f a b", []string{"rm", "-f", "a", "b"}},
		{"single quotes", `sed -i 's/a b/c/' f`, []string{"sed", "-i", "s/a b/c/", "f"}},
		{"double quotes with escape", `echo "a \"b\" c" f`, []string{"echo", `a "b" c`, "f"}},
		{"backslash escape", `rm a\ b`, []string{"rm", "a b"}},
		{"redirect spaced", "echo x > f", []string{"echo", "x", ">", "f"}},
		{"redirect attached", "echo x>f", []string{"echo", "x", ">", "f"}},
		{"append", "echo x >>f", []string{"echo", "x", ">>", "f"}},
		{"fd redirect", "cmd 2>/dev/null", []string{"cmd", "2>", "/dev/null"}},
		{"fd dup", "cmd 2>&1", []string{"cmd", "2>", "&1"}},
		{"combined redirect", "cmd &>out", []string{"cmd", "&>", "out"}},
		{"combined append", "cmd &>> out", []string{"cmd", "&>>", "out"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenizeShellWords(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tokenizeShellWords(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsWipnoteStorePath(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{".wipnote", true},
		{".wipnote/", true},
		{".wipnote/features/feat-1.html", true},
		{"./.wipnote/features/feat-1.html", true},
		{"/abs/proj/.wipnote/bugs/bug-1.html", true},
		{"$ROOT/.wipnote/x", true},
		{"--output=.wipnote/x", true},
		{"proj/.wipnote", true},
		{".wipnote/x)", true},
		{"", false},
		{"README.md", false},
		{"my.wipnote/x", false},
		{".wipnote-backup/x", false},
		{"s|.wipnote/|x|", false},
		{"never touch .wipnote/ by hand", false},
	}
	for _, tc := range tests {
		t.Run(tc.arg, func(t *testing.T) {
			if got := isWipnoteStorePath(tc.arg); got != tc.want {
				t.Errorf("isWipnoteStorePath(%q) = %v, want %v", tc.arg, got, tc.want)
			}
		})
	}
}
