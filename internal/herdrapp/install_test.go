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
	for _, rel := range []string{"layouts/wipnote-board.json", "mods/wipnote-board/main.js", "mods/wipnote-board/mod.json", "mods/wipnote-pane-header/main.js", "mods/wipnote-pane-header/mod.json"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "RECIPE.md")); !os.IsNotExist(err) {
		t.Error("the recipe must stay out of the home")
	}
}

func TestInstall_NeverOverwrites(t *testing.T) {
	for _, rel := range []string{"layouts/wipnote-board.json", "mods/wipnote-board", "mods/wipnote-pane-header"} {
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
			// All or nothing: the other target must not appear either.
			entries, _ := filepath.Glob(filepath.Join(home, "*", "wipnote-*"))
			if len(entries) != 1 {
				t.Fatalf("a refused install wrote files: %q", entries)
			}
		})
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

// The layout must name the mod it ships and set no look of its own, so it
// follows the person's theme.
func TestLayout_NamesItsModAndNoLook(t *testing.T) {
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
	if !strings.Contains(string(data), `"mod": "`+LayoutName+`"`) {
		t.Error("layout does not show its mod")
	}
}

// The layout frames every pane, a lone one too, with the header mod it ships.
func TestLayout_HeaderOnEveryPane(t *testing.T) {
	data, err := assets.ReadFile(assetRoot + "/layouts/" + LayoutName + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Children []struct {
			Children []struct {
				ID      string `json:"id"`
				Content struct {
					Pane struct {
						Lone   bool `json:"lone"`
						Header struct {
							Content  map[string]string `json:"content"`
							Position string            `json:"position"`
							Reveal   string            `json:"reveal"`
						} `json:"header"`
					} `json:"pane"`
				} `json:"content"`
			} `json:"children"`
		} `json:"children"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	for _, col := range root.Children {
		for _, b := range col.Children {
			if b.ID != "main" {
				continue
			}
			p := b.Content.Pane
			if !p.Lone || p.Header.Content["mod"] != HeaderMod || p.Header.Position != "top" || p.Header.Reveal != "always" {
				t.Fatalf("main pane = %+v", p)
			}
			return
		}
	}
	t.Fatal("no main block")
}
