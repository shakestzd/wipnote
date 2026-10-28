package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noticeEnv gives a test its own home and project, with Claude Code's
// settings at ~/.claude (CLAUDE_CONFIG_DIR unset).
func noticeEnv(t *testing.T) (home, project string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return home, t.TempDir()
}

func writeSettings(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// modSettings is a settings.json with the retired mod's userConfig values.
func modSettings(key, options string) string {
	return `{"theme":"dark","pluginConfigs":{"wipnote-orchestra@inline":{"options":{"timeZone":"UTC"}},"` +
		key + `":{"options":` + options + `}}}`
}

func TestSettingsNotice_OnceForModUsers(t *testing.T) {
	home, project := noticeEnv(t)
	key := retiredPaneMod + "@inline"
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"),
		modSettings(key, `{"layout":"tile","maxPanes":4,"viewer":"plain","claudePane":"auto","ccxRunsDir":"/r"}`))

	var first bytes.Buffer
	if !maybeShowModSettingsNotice(&first, project) {
		t.Fatal("a user with the mod's settings must see the notice")
	}
	out := first.String()
	for _, want := range []string{
		key, "delegate.layout", `"tile"`, "delegate.maxPanes", "delegate.ccxRunsDir",
		"viewer", "wipnote delegate view", "claudePane", "wipnote-orchestra",
		"~/.wipnote/config.json", `{"delegate":{"ccxRunsDir":"/r","layout":"tile","maxPanes":4}}`,
		"did not move", "once",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("notice lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "timeZone") {
		t.Errorf("notice names another plugin's option:\n%s", out)
	}

	var second bytes.Buffer
	if maybeShowModSettingsNotice(&second, project) || second.Len() != 0 {
		t.Errorf("the notice must show once; second launch printed:\n%s", second.String())
	}
}

// The mod's options in the project's local settings count too, and the
// plugin key without @inline is the same mod.
func TestSettingsNotice_ProjectSettings(t *testing.T) {
	_, project := noticeEnv(t)
	writeSettings(t, filepath.Join(project, ".claude", "settings.local.json"),
		modSettings(retiredPaneMod, `{"closePane":"never"}`))
	var out bytes.Buffer
	if !maybeShowModSettingsNotice(&out, project) || !strings.Contains(out.String(), "delegate.closePane") {
		t.Errorf("project-level mod settings must show the notice:\n%s", out.String())
	}
}

func TestSettingsNotice_HonoursClaudeConfigDir(t *testing.T) {
	_, project := noticeEnv(t)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	writeSettings(t, filepath.Join(cfg, "settings.json"), modSettings(retiredPaneMod, `{"tailLines":50}`))
	var out bytes.Buffer
	if !maybeShowModSettingsNotice(&out, project) {
		t.Errorf("settings under CLAUDE_CONFIG_DIR must be read")
	}
}

// No notice, and no marker, for anyone without the mod's settings, and
// never a failure on a bad file.
func TestSettingsNotice_SilentWithoutModSettings(t *testing.T) {
	cases := map[string]string{
		"no settings file":   "",
		"other plugins only": `{"pluginConfigs":{"wipnote-orchestra":{"options":{"timeZone":"UTC"}}}}`,
		"empty options":      modSettings(retiredPaneMod, `{}`),
		"unknown keys only":  modSettings(retiredPaneMod, `{"bogus":1}`),
		"broken JSON":        `{"pluginConfigs":`,
		"wrong shapes":       `{"pluginConfigs":{"` + retiredPaneMod + `":{"options":[1,2]}}}`,
		"a similar name":     modSettings(retiredPaneMod+"-fork", `{"layout":"tile"}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			home, project := noticeEnv(t)
			if body != "" {
				writeSettings(t, filepath.Join(home, ".claude", "settings.json"), body)
			}
			var out bytes.Buffer
			if maybeShowModSettingsNotice(&out, project) || out.Len() != 0 {
				t.Errorf("unexpected notice:\n%s", out.String())
			}
			if _, err := os.Stat(modSettingsNoticeMarker()); !os.IsNotExist(err) {
				t.Errorf("marker written without a notice (stat err %v)", err)
			}
		})
	}
}

// A settings file too large to be Claude Code's own is not read: the check
// stays cheap on every launch.
func TestSettingsNotice_SkipsAHugeFile(t *testing.T) {
	home, project := noticeEnv(t)
	pad := strings.Repeat(" ", modSettingsMaxBytes)
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), modSettings(retiredPaneMod, `{"layout":"tile"}`)+pad)
	var out bytes.Buffer
	if maybeShowModSettingsNotice(&out, project) {
		t.Errorf("a file over %d bytes must be skipped", modSettingsMaxBytes)
	}
}
