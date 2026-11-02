package herdrapp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall_WritesLayoutAndModsOnly(t *testing.T) {
	home := t.TempDir()
	wrote, err := Install(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 3 {
		t.Fatalf("wrote = %q", wrote)
	}
	for _, rel := range []string{"layouts/wipnote-main.json", "mods/wipnote-agents/main.js", "mods/wipnote-agents/mod.json", "mods/wipnote-pane-header/main.js", "mods/wipnote-pane-header/mod.json"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "RECIPE.md")); !os.IsNotExist(err) {
		t.Error("the recipe must stay out of the home")
	}
}

func TestInstall_NeverOverwrites(t *testing.T) {
	for _, rel := range []string{"layouts/wipnote-main.json", "mods/wipnote-agents", "mods/wipnote-pane-header"} {
		t.Run(rel, func(t *testing.T) {
			home := t.TempDir()
			target := filepath.Join(home, rel)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("mine"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Install(home)
			var c *Conflict
			if !errors.As(err, &c) || c.Path != target {
				t.Fatalf("err = %v, want a conflict on %s", err, target)
			}
			if data, _ := os.ReadFile(target); string(data) != "mine" {
				t.Fatalf("the person's file changed to %q", data)
			}
			// All or nothing: the other targets must not appear either.
			entries, _ := filepath.Glob(filepath.Join(home, "*", "wipnote-*"))
			if len(entries) != 1 {
				t.Fatalf("a refused install wrote files: %q", entries)
			}
		})
	}
}

// Replace keeps what it moves, outside the folders the app loads, and installs.
func TestReplace_MovesExistingAsideAndInstalls(t *testing.T) {
	home := t.TempDir()
	old := filepath.Join(home, "mods", AgentsMod, "main.js")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrote, moved, err := Replace(home, "stamp")
	if err != nil || len(wrote) != 3 {
		t.Fatalf("wrote %q, err %v", wrote, err)
	}
	if want := filepath.Join(home, "wipnote-replaced-stamp"); moved != want {
		t.Fatalf("moved = %q, want %q", moved, want)
	}
	if data, _ := os.ReadFile(filepath.Join(moved, "mods", AgentsMod, "main.js")); string(data) != "mine" {
		t.Fatalf("moved file holds %q", data)
	}
	if data, _ := os.ReadFile(old); string(data) == "mine" {
		t.Fatal("the new mod was not installed")
	}
}

func TestReplace_NothingToMove(t *testing.T) {
	if _, moved, err := Replace(t.TempDir(), "stamp"); err != nil || moved != "" {
		t.Fatalf("moved %q, err %v", moved, err)
	}
}

func TestHome(t *testing.T) {
	get := func(v string) func(string) string { return func(string) string { return v } }
	if got := Home(get("/custom"), "/u"); got != "/custom" {
		t.Fatalf("Home = %q", got)
	}
	if got := Home(get(""), "/u"); got != filepath.Join("/u", ".config", "herdr-app") {
		t.Fatalf("Home = %q", got)
	}
}

// The layout names the mods it ships, keeps Herdr's own tab bar so the workers
// tab shows, and sets no look of its own, so it follows the person's theme.
func TestLayout_NamesItsModsAndNoLook(t *testing.T) {
	data, err := assets.ReadFile(assetRoot + "/layouts/" + LayoutName + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("layout is not JSON: %v", err)
	}
	for _, key := range []string{"theme", "colors", "font", "ui_font"} {
		if _, set := root[key]; set {
			t.Errorf("layout sets %q; it must follow the person's look", key)
		}
	}
	for _, mod := range []string{AgentsMod, HeaderMod, "tabs", "spaces"} {
		if !strings.Contains(string(data), `"mod": "`+mod+`"`) {
			t.Errorf("layout does not show mod %s", mod)
		}
	}
	if strings.Contains(string(data), `"mod": "agents"`) {
		t.Error("layout still shows Herdr's agents mod")
	}
}

// The agents mod keeps only a worker's nested row, and the rows it hides still
// count for their parent's status: the filter comes after this.listed is built.
func TestAgentsMod_NestsWorkersOnly(t *testing.T) {
	data, err := assets.ReadFile(assetRoot + "/mods/" + AgentsMod + "/main.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	listed := strings.Index(src, "this.listed = new Map(")
	filter := strings.Index(src, "list = list.filter((a) => !this.listed.has(parentOf.get(a.address)))")
	if listed < 0 || filter < listed {
		t.Fatal("wipnote-agents must filter nested workers after building this.listed")
	}
}
