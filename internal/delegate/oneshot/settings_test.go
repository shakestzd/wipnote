package oneshot

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// fixes.test.ts "layout auto keeps the legacy direction behaviour": auto with
// direction down is a fixed split down (no tile registry); auto with right
// tiles; right and down are fixed; tile tiles.
func TestSettings_LayoutAutoKeepsLegacyDirection(t *testing.T) {
	cases := []struct {
		layout, direction string
		fixed             bool
		dir               herdr.Direction
	}{
		{"auto", "down", true, herdr.Down},
		{"auto", "right", false, ""},
		{"", "down", true, herdr.Down},
		{"", "", false, ""},
		{"tile", "down", false, ""},
		{"right", "down", true, herdr.Right},
		{"down", "right", true, herdr.Down},
	}
	for _, c := range cases {
		s := DefaultSettings()
		s.Layout, s.Direction = c.layout, c.direction
		fixed, dir := s.Placement()
		if fixed != c.fixed || dir != c.dir {
			t.Errorf("layout %q direction %q: fixed %v dir %q, want %v %q", c.layout, c.direction, fixed, dir, c.fixed, c.dir)
		}
	}
}

// The mod's userConfig maps to .wipnote/config.json "delegate"; a bad or
// missing value keeps the mod's default, and ccxRunsDir has no default. A
// ccxRunsDir in the committed project file is ignored (a host path).
func TestSettings_LoadFromProjectConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	s := LoadSettings(dir, io.Discard)
	if s != DefaultSettings() || s.CcxRunsDir != "" || s.MaxReturnLines != 60 || s.TailLines != 200 || s.MaxPanes != 6 {
		t.Fatalf("defaults: %+v", s)
	}
	must(t, os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755))
	cfg := `{"delegate": {"layout": "down", "maxPanes": 2, "closePane": "never", "closeDelay": 0, "tailLines": 5,
		"maxReturnLines": 0, "leanProfile": false, "leanProfileName": "x y", "ccxRunsDir": " /data/ccx $x ",
		"ccxWaitSeconds": 9, "keepDays": 2, "bogus": 1}}`
	must(t, os.WriteFile(filepath.Join(dir, ".wipnote", "config.json"), []byte(cfg), 0o600))
	s = LoadSettings(dir, io.Discard)
	want := Settings{Layout: "down", Direction: "right", MaxPanes: 2, ClosePane: "never", CloseDelay: 0, TailLines: 5,
		MaxReturnLines: 0, LeanProfile: false, LeanProfileName: "xy", CcxWaitSeconds: 9, KeepDays: 2}
	if s != want {
		t.Fatalf("loaded %+v\nwant %+v", s, want)
	}
	must(t, os.WriteFile(filepath.Join(dir, ".wipnote", "config.json"), []byte(`{"delegate": {"maxPanes": 0, "closePane": "sometimes", "maxReturnLines": -1}}`), 0o600))
	if s := LoadSettings(dir, io.Discard); s.MaxPanes != 6 || s.ClosePane != "always" || s.MaxReturnLines != 60 {
		t.Fatalf("bad values not defaulted: %+v", s)
	}
}

// ccxWaitSeconds (30) and keepDays (7) replace the mod's fixed wait and its
// CODEX_HERDR_KEEP_DAYS sweep age. A value under 1 keeps the default, and
// the old CODEX_HERDR_* env names are not read.
func TestSettings_CcxWaitAndKeepDays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HERDR_KEEP_DAYS", "1")
	t.Setenv("CODEX_HERDR_CCX_WAIT", "1")
	t.Setenv("CODEX_HERDR_CCX_WAIT_SECONDS", "1")
	dir := t.TempDir()
	if s := LoadSettings(dir, io.Discard); s.CcxWaitSeconds != 30 || s.KeepDays != 7 {
		t.Fatalf("defaults (old env must not count): wait %d keep %d", s.CcxWaitSeconds, s.KeepDays)
	}
	must(t, os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755))
	cfg := filepath.Join(dir, ".wipnote", "config.json")
	must(t, os.WriteFile(cfg, []byte(`{"delegate": {"ccxWaitSeconds": 0, "keepDays": 0}}`), 0o600))
	if s := LoadSettings(dir, io.Discard); s.CcxWaitSeconds != 30 || s.KeepDays != 7 {
		t.Fatalf("values under 1 not defaulted: wait %d keep %d", s.CcxWaitSeconds, s.KeepDays)
	}
	must(t, os.WriteFile(cfg, []byte(`{"delegate": {"ccxWaitSeconds": 45, "keepDays": 14}}`), 0o600))
	if s := LoadSettings(dir, io.Discard); s.CcxWaitSeconds != 45 || s.KeepDays != 14 {
		t.Fatalf("set values not read: wait %d keep %d", s.CcxWaitSeconds, s.KeepDays)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
