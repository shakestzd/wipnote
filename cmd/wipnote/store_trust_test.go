package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
)

// trustStore makes an app store for a plain folder under a temp home, with
// Claude Code's config dir pointed at the same temp home.
func trustStore(t *testing.T) (owner, cfgPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	project := filepath.Join(home, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	owner, _, err := paths.CreateExternalStore(project, paths.OriginLauncher, nil)
	if err != nil {
		t.Fatal(err)
	}
	return owner, oneshot.ClaudeConfigPath()
}

func TestStoreTrust_SetsOnlyTheStoreFlagAndIsIdempotent(t *testing.T) {
	owner, cfgPath := trustStore(t)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"numStartups":12345678901234567890,"projects":{"/other":{"x":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if w := storeTrustWarning(owner); !strings.Contains(w, "wipnote store trust") {
		t.Fatalf("no warning for an untrusted store: %q", w)
	}

	var out bytes.Buffer
	if err := runStoreTrust(&out, owner, true); err != nil {
		t.Fatal(err)
	}
	if ok, _ := oneshot.TrustAccepted(owner); ok || !strings.Contains(out.String(), "Dry run") {
		t.Fatalf("dry run changed trust or said nothing: %s", out.String())
	}

	out.Reset()
	if err := runStoreTrust(&out, owner, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfgPath)
	var cfg struct {
		NumStartups json.Number `json:"numStartups"`
		Projects    map[string]map[string]any
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Projects[filepath.Clean(owner)]["hasTrustDialogAccepted"] != true {
		t.Fatalf("store not trusted: %s", data)
	}
	if cfg.NumStartups.String() != "12345678901234567890" || cfg.Projects["/other"]["hasTrustDialogAccepted"] != nil {
		t.Fatalf("another key changed: %s", data)
	}
	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	if _, ok := all["hasCompletedOnboarding"]; ok || len(all) != 2 || len(cfg.Projects["/other"]) != 1 {
		t.Fatalf("a key beyond the store flag changed: %s", data)
	}
	if w := storeTrustWarning(owner); w != "" {
		t.Fatalf("warning after trust: %q", w)
	}

	out.Reset()
	if err := runStoreTrust(&out, owner, false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(data, after) || !strings.Contains(out.String(), "already trusts") {
		t.Fatalf("second run changed the config or did not say so: %s", out.String())
	}
}

func TestStoreTrust_InRepoStoreNeedsNothing(t *testing.T) {
	_, cfgPath := trustStore(t)
	inRepo := t.TempDir()
	if w := storeTrustWarning(inRepo); w != "" {
		t.Fatalf("warning for an in-repo store: %q", w)
	}
	var out bytes.Buffer
	if err := runStoreTrust(&out, inRepo, false); err != nil || !strings.Contains(out.String(), "needs no trust") {
		t.Fatalf("in-repo store: %v %s", err, out.String())
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("the config was written for an in-repo store")
	}
}

// Trust on a folder above the store, such as the home folder, does not cover
// it: Claude Code still asks about a nested repository. The command must write
// the store's own flag and the launch must still warn until it has.
func TestStoreTrust_TrustedParentDoesNotCoverTheStore(t *testing.T) {
	owner, _ := trustStore(t)
	for dir := filepath.Dir(owner); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if err := oneshot.PreseedTrust(dir); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := oneshot.TrustAccepted(owner); ok || err != nil {
		t.Fatalf("a trusted parent covered the store: %v %v", ok, err)
	}
	if w := storeTrustWarning(owner); w == "" {
		t.Fatal("no warning although only parents are trusted")
	}
	var out bytes.Buffer
	if err := runStoreTrust(&out, owner, false); err != nil || !strings.Contains(out.String(), "Marked") {
		t.Fatalf("store trust under a trusted parent: %v %s", err, out.String())
	}
	if ok, err := oneshot.TrustAccepted(owner); !ok || err != nil {
		t.Fatalf("the store's own flag was not set: %v %v", ok, err)
	}
	if w := storeTrustWarning(owner); w != "" {
		t.Fatalf("warning after the store was trusted: %q", w)
	}
}

// A config holding null is reported, not a panic, and a relative folder never
// becomes a "." key that would match every relative path.
func TestStoreTrust_NullConfigAndRelativeFolder(t *testing.T) {
	owner, cfgPath := trustStore(t)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runStoreTrust(&out, owner, false); err == nil || !strings.Contains(err.Error(), "null") {
		t.Fatalf("null config: %v", err)
	}
	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	t.Chdir(owner)
	if err := oneshot.TrustFolder("."); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(data), `"."`) {
		t.Fatalf("a relative key was written: %s", data)
	}
	if ok, _ := oneshot.TrustAccepted("."); !ok {
		t.Fatal("the store is not trusted by its relative path")
	}
	if _, err := oneshot.TrustAccepted(""); err == nil {
		t.Fatal("an empty folder was accepted")
	}
}

// A dry run in a folder with no store makes no store and no config.
func TestStoreTrust_DryRunInPlainFolderCreatesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	plain := filepath.Join(home, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(plain)
	cmd := storeTrustCmd()
	cmd.SetArgs([]string{"--dry-run"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	_ = cmd.Execute() // "no store" is a fine answer; creating one is not
	for _, p := range []string{filepath.Join(home, ".wipnote"), filepath.Join(home, "claude"), filepath.Join(plain, ".wipnote")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("dry run created %s", p)
		}
	}
}
