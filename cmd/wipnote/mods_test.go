package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// writeMod lays out <dir>/<name>/.claude-plugin/plugin.json.
func writeMod(t *testing.T, dir, name string) {
	t.Helper()
	manifest := filepath.Join(dir, name, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolateModsLookup keeps the shared-tree fallback from finding this
// checkout's mods/ (from the working directory) or the real home's.
func isolateModsLookup(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestResolveModsDirBesideThePlugin(t *testing.T) {
	isolateModsLookup(t)
	t.Setenv("WIPNOTE_NO_MODS", "")
	t.Setenv("WIPNOTE_MODS_DIR", "")
	share := t.TempDir()
	plugin := filepath.Join(share, "plugin")
	if err := os.MkdirAll(plugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("no mods folder: got %q, want none", got)
	}
	if err := os.MkdirAll(filepath.Join(share, "mods", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("a mods folder with no mod in it: got %q, want none", got)
	}
	writeMod(t, filepath.Join(share, "mods"), "wipnote-orchestra")
	if got, want := resolveModsDir(plugin), filepath.Join(share, "mods"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A plugin install with no mods beside it (one laid down before mods
// shipped) falls back to the shared-tree order, here ~/.local/share.
func TestResolveModsDirFallsBackToTheSharedTrees(t *testing.T) {
	t.Setenv("WIPNOTE_NO_MODS", "")
	t.Setenv("WIPNOTE_MODS_DIR", "")
	home := isolateModsLookup(t)
	shared := filepath.Join(home, ".local", "share", "wipnote", "mods")
	writeMod(t, shared, "wipnote-orchestra")
	stale := filepath.Join(t.TempDir(), "plugin") // no mods/ beside it
	if got := resolveModsDir(stale); got != shared {
		t.Errorf("got %q, want the shared tree %q", got, shared)
	}
	if got := resolveModsDir(""); got != shared {
		t.Errorf("no plugin dir: got %q, want the shared tree %q", got, shared)
	}
}

func TestResolveModsDirOverrideAndOptOut(t *testing.T) {
	isolateModsLookup(t)
	share := t.TempDir()
	writeMod(t, filepath.Join(share, "mods"), "wipnote-orchestra")
	plugin := filepath.Join(share, "plugin")
	other := t.TempDir()
	writeMod(t, other, "mine")

	t.Setenv("WIPNOTE_NO_MODS", "")
	t.Setenv("WIPNOTE_MODS_DIR", other)
	if got := resolveModsDir(plugin); got != other {
		t.Errorf("override: got %q, want %q", got, other)
	}
	// A relative override comes back absolute: Claude starts in another folder.
	t.Chdir(filepath.Dir(other))
	t.Setenv("WIPNOTE_MODS_DIR", filepath.Base(other))
	if got := resolveModsDir(plugin); got != other {
		t.Errorf("relative override: got %q, want %q", got, other)
	}
	t.Setenv("WIPNOTE_MODS_DIR", t.TempDir())
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("override with no mod: got %q, want none (not the plugin's own)", got)
	}
	t.Setenv("WIPNOTE_MODS_DIR", "")
	t.Setenv("WIPNOTE_NO_MODS", "1")
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("opted out: got %q, want none", got)
	}
	t.Setenv("WIPNOTE_NO_MODS", "0")
	if got := resolveModsDir(plugin); got == "" {
		t.Errorf("WIPNOTE_NO_MODS=0 must still load the mods")
	}
}

func TestBuildClaudeArgsLoadsModsAfterThePlugin(t *testing.T) {
	args := buildClaudeArgs(LaunchOpts{PluginDir: "/s/plugin", ModsDir: "/s/mods"}, "")
	want := []string{"--plugin-dir", "/s/plugin", "--plugin-dir", "/s/mods"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestModsSystemPromptJoinsEachModsRule(t *testing.T) {
	dir := t.TempDir()
	writeMod(t, dir, "b-mod")
	writeMod(t, dir, "a-mod")
	writeMod(t, dir, "no-rule")
	for name, rule := range map[string]string{"a-mod": "Rule A.\n", "b-mod": "  Rule B.  "} {
		if err := os.WriteFile(filepath.Join(dir, name, modsPromptFile), []byte(rule), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := modsSystemPrompt(dir), "Rule A.\n\nRule B."; got != want {
		t.Errorf("modsSystemPrompt = %q, want %q", got, want)
	}
	if got := modsSystemPrompt(""); got != "" {
		t.Errorf("modsSystemPrompt(\"\") = %q, want empty", got)
	}
}

// The repository's mods carry the rules their prompt.compose hooks used to send.
// No mod asks for wip:* fences: reply blocks are lulawrite's, so such a rule
// would ask for blocks nothing draws (feat-fd3637f9).
func TestRepoModsCarryTheirRules(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	got := modsSystemPrompt(filepath.Join(root, "mods"))
	if !strings.Contains(got, "wipnote-ref") {
		t.Errorf("mods rules lack wipnote-ref:\n%s", got)
	}
	for _, gone := range []string{"wip:compare", "wip:steps", "wip:callout"} {
		if strings.Contains(got, gone) {
			t.Errorf("mods rules still ask for %s", gone)
		}
	}
}

// The repository's own mods/ folder is what --dev and the release ship.
func TestRepoModsFolderHoldsMods(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if !isModsTree(filepath.Join(root, "mods")) {
		t.Errorf("%s/mods holds no mod", root)
	}
}

func TestMirrorModsTreeCopiesTrackedRuntimeFilesOnly(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "mods")
	if err := mirrorModsTree(root, dst); err != nil {
		t.Fatalf("mirrorModsTree: %v", err)
	}
	if !isModsTree(dst) {
		t.Fatalf("%s holds no mod after the mirror", dst)
	}
	for _, want := range []string{
		filepath.Join("wipnote-orchestra", "hooks", "register.tsx"),
	} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	err = filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dst, path)
		if !info.IsDir() && isModTestFile(rel) {
			t.Errorf("test file mirrored: %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// modsCheckout lays out a git checkout whose index tracks the given files
// under mods/, each holding its own path as content.
func modsCheckout(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	for _, name := range files {
		path := filepath.Join(root, "mods", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	return root
}

// A second mirror of an unchanged checkout must not touch a file: Claude Code
// reloads a mod whose files change on disk, in every open session.
func TestMirrorModsTreeLeavesUnchangedFilesUntouched(t *testing.T) {
	root := modsCheckout(t, "one/.claude-plugin/plugin.json", "one/hooks/register.tsx")
	dst := filepath.Join(t.TempDir(), "mods")
	if err := mirrorModsTree(root, dst); err != nil {
		t.Fatalf("first mirror: %v", err)
	}
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	files := []string{
		filepath.Join(dst, "one", ".claude-plugin", "plugin.json"),
		filepath.Join(dst, "one", "hooks", "register.tsx"),
	}
	for _, f := range files {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := mirrorModsTree(root, dst); err != nil {
		t.Fatalf("second mirror: %v", err)
	}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(old) {
			t.Errorf("%s was rewritten with no change in the source", f)
		}
	}
	left, err := filepath.Glob(filepath.Join(dst, "one", "hooks", ".mirror-*"))
	if err != nil || len(left) != 0 {
		t.Errorf("temporary files left behind: %v (%v)", left, err)
	}
}

func TestMirrorModsTreeWritesChangesAndRemovesWhatLeftTheSource(t *testing.T) {
	root := modsCheckout(t, "one/.claude-plugin/plugin.json", "one/hooks/register.tsx", "gone/.claude-plugin/plugin.json", "gone/hooks/register.tsx")
	dst := filepath.Join(t.TempDir(), "mods")
	if err := mirrorModsTree(root, dst); err != nil {
		t.Fatalf("first mirror: %v", err)
	}
	// Claude Code's own generated types, and a stray file nobody tracks.
	engine := filepath.Join(dst, "one", ".claude-plugin", "types", "claude-code", "index.d.ts")
	stray := filepath.Join(dst, "one", "hooks", "old.tsx")
	for _, f := range []string{engine, stray} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "mods", "one", "hooks", "register.tsx"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "rm", "-rq", "--cached", "mods/gone").CombinedOutput(); err != nil {
		t.Fatalf("git rm: %v: %s", err, out)
	}
	if err := mirrorModsTree(root, dst); err != nil {
		t.Fatalf("second mirror: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "one", "hooks", "register.tsx")); string(got) != "new" {
		t.Errorf("changed file not rewritten: %q", got)
	}
	if _, err := os.Stat(engine); err != nil {
		t.Errorf("the engine's generated types must stay: %v", err)
	}
	for _, f := range []string{stray, filepath.Join(dst, "gone")} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s must be removed (err %v)", f, err)
		}
	}
}

func TestMirrorModsTreeSkipsACheckoutWithoutMods(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "mods")
	writeMod(t, dst, "kept")
	if err := mirrorModsTree(t.TempDir(), dst); err != nil {
		t.Fatalf("no mods/ must not fail the build: %v", err)
	}
	if !isModsTree(dst) {
		t.Errorf("an existing mods tree must be left as it is")
	}
}

func TestIsModTestFile(t *testing.T) {
	for rel, want := range map[string]bool{
		"a/hooks/register.ts":      false,
		"a/hooks/register.test.ts": true,
		"a/test/run.sh":            true,
		"a/tests/band.test.tsx":    true,
		"a/viewer/codex_view.py":   false,
		"a/hooks/latest.ts":        false,
	} {
		if got := isModTestFile(rel); got != want {
			t.Errorf("isModTestFile(%q) = %v, want %v", rel, got, want)
		}
	}
}

// TestMods_NoCodexHerdrPane: the retired mod is gone from mods/, and no file
// outside the exempt paths names it. Exempt: work items (.wipnote/), design
// records (docs/design/), CLAUDE.md (the owner edits it) and the CHANGELOG
// history. The mod's settings notice spells the name in parts.
func TestMods_NoCodexHerdrPane(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "mods", retiredPaneMod)); !os.IsNotExist(err) {
		t.Errorf("mods/%s must be deleted (stat err %v)", retiredPaneMod, err)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	for _, rel := range bytes.Split(out, []byte{0}) {
		name := string(rel)
		if name == "" || retiredModExempt(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue // tracked but deleted in the working tree
		}
		if bytes.Contains(data, []byte(retiredPaneMod)) {
			t.Errorf("%s names %s", name, retiredPaneMod)
		}
	}
}

func retiredModExempt(rel string) bool {
	for _, prefix := range []string{".wipnote/", "docs/design/"} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return rel == "CLAUDE.md" || rel == "CHANGELOG.md"
}

// The guard itself sees a file that names the mod: a two-sided check.
func TestRetiredModExempt(t *testing.T) {
	for rel, want := range map[string]bool{
		".wipnote/features/feat-1.html":    true,
		"docs/design/delegate.md":          true,
		"CLAUDE.md":                        true,
		"CHANGELOG.md":                     true,
		"plugin/agents/researcher.md":      false,
		"docs/guide.md":                    false,
		"mods/wipnote-orchestra/README.md": false,
	} {
		if got := retiredModExempt(rel); got != want {
			t.Errorf("retiredModExempt(%q) = %v, want %v", rel, got, want)
		}
	}
}

// A lean delegate worker is not an orchestrator: no orchestrator prompt and no
// research routing, but the mods' rules stay.
func TestComposeSystemPrompt_NoOrchestratorKeepsMods(t *testing.T) {
	dir := t.TempDir()
	writeMod(t, dir, "a")
	if err := os.WriteFile(filepath.Join(dir, "a", modsPromptFile), []byte("Rule A."), 0o644); err != nil {
		t.Fatal(err)
	}
	full := composeSystemPrompt(LaunchOpts{InjectSystemPrompt: true, ModsDir: dir})
	if !strings.Contains(full, strings.TrimSpace(researchRoutingContent)) || !strings.HasSuffix(strings.TrimSpace(full), "Rule A.") {
		t.Fatalf("full prompt lacks routing or the mod rule")
	}
	lean := composeSystemPrompt(LaunchOpts{InjectSystemPrompt: true, NoOrchestrator: true, ModsDir: dir})
	if lean != "Rule A.\n" {
		t.Fatalf("lean prompt = %q, want only the mod rule", lean)
	}
}

// The questions mod ships its manifest and hooks files.
func TestQuestionsModHasManifestAndHooks(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(root, "mods", "wipnote-questions")
	for _, rel := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		filepath.Join("hooks", "hooks.json"),
		filepath.Join("hooks", "register.tsx"),
	} {
		if _, err := os.Stat(filepath.Join(mod, rel)); err != nil {
			t.Errorf("wipnote-questions lacks %s: %v", rel, err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(mod, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"name": "wipnote-questions"`) {
		t.Errorf("plugin.json does not name wipnote-questions:\n%s", manifest)
	}
	hooks, err := os.ReadFile(filepath.Join(mod, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hooks), "./register.tsx") {
		t.Errorf("hooks.json does not load register.tsx:\n%s", hooks)
	}
}
