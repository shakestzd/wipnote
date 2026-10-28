package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// retiredPaneMod is the name of the Claude Code mod that wipnote delegate
// replaced. It is spelled in parts so that TestMods_NoCodexHerdrPane can
// search the tree for the whole name: this notice is the only code that
// still has to know it.
var retiredPaneMod = "codex-" + "herdr-pane"

// modSettingsMaxBytes caps each settings file the notice reads, so the check
// stays cheap on every launch. Claude Code's own files are far smaller.
const modSettingsMaxBytes = 1 << 20

// retiredModOptions lists the mod's userConfig keys in its manifest order.
// A key with a delegate setting maps to delegate.<same key>; the others say
// what replaced them.
var retiredModOptions = []struct{ key, gone string }{
	{key: "direction"}, {key: "layout"}, {key: "maxPanes"}, {key: "closePane"},
	{key: "closeDelay"}, {key: "viewer", gone: "no setting: `wipnote delegate view <run-id>` is the only viewer"},
	{key: "tailLines"}, {key: "maxReturnLines"}, {key: "leanProfile"},
	{key: "leanProfileName"}, {key: "ccxRunsDir"},
	{key: "claudePane", gone: "no setting: the wipnote-orchestra band shows runs outside herdr"},
}

// modSettingsNoticeMarker is the per-user file that records the notice was
// shown: ~/.wipnote/.mod-settings-notice-shown, or "" with no home.
func modSettingsNoticeMarker() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".wipnote", ".mod-settings-notice-shown")
}

// maybeShowModSettingsNotice writes a one-time notice to w when Claude
// Code's settings still hold the retired mod's userConfig values, saying
// which wipnote delegate setting each one maps to. It moves nothing. It
// fails open: an unreadable, huge or broken file is skipped, and a marker
// that cannot be written only means the notice shows again. It reports
// whether it wrote the notice.
func maybeShowModSettingsNotice(w io.Writer, projectRoot string) bool {
	marker := modSettingsNoticeMarker()
	if marker == "" {
		return false
	}
	if _, err := os.Stat(marker); err == nil {
		return false
	}
	for _, path := range claudeSettingsFiles(projectRoot) {
		key, options := retiredModSettings(path)
		if len(options) == 0 {
			continue
		}
		fmt.Fprint(w, modSettingsNotice(key, path, options))
		if os.MkdirAll(filepath.Dir(marker), 0o755) == nil {
			_ = os.WriteFile(marker, []byte("shown\n"), 0o644)
		}
		return true
	}
	return false
}

// claudeSettingsFiles are the Claude Code settings files that can hold
// pluginConfigs: the user file ($CLAUDE_CONFIG_DIR or ~/.claude), then the
// project's shared and local files.
func claudeSettingsFiles(projectRoot string) []string {
	var files []string
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		files = append(files, filepath.Join(dir, "settings.json"))
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		files = append(files, filepath.Join(home, ".claude", "settings.json"))
	}
	if projectRoot != "" {
		files = append(files,
			filepath.Join(projectRoot, ".claude", "settings.json"),
			filepath.Join(projectRoot, ".claude", "settings.local.json"))
	}
	return files
}

// retiredModSettings returns the pluginConfigs key and the known options
// the retired mod has in one settings file (its name alone or name@source).
// Anything unreadable or of the wrong shape yields none.
func retiredModSettings(path string) (string, map[string]json.RawMessage) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > modSettingsMaxBytes {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	var settings struct {
		PluginConfigs map[string]json.RawMessage `json:"pluginConfigs"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return "", nil
	}
	for key, raw := range settings.PluginConfigs {
		if key != retiredPaneMod && !strings.HasPrefix(key, retiredPaneMod+"@") {
			continue
		}
		var entry struct {
			Options map[string]json.RawMessage `json:"options"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		if known := knownModOptions(entry.Options); len(known) > 0 {
			return key, known
		}
	}
	return "", nil
}

// knownModOptions keeps the options that are the mod's own keys.
func knownModOptions(options map[string]json.RawMessage) map[string]json.RawMessage {
	known := map[string]json.RawMessage{}
	for _, opt := range retiredModOptions {
		if v, ok := options[opt.key]; ok {
			known[opt.key] = v
		}
	}
	return known
}

// modSettingsNotice is the notice text: one line per option set, then the
// delegate object to paste into ~/.wipnote/config.json.
func modSettingsNotice(key, path string, options map[string]json.RawMessage) string {
	var b strings.Builder
	b.WriteString("\n  wipnote: the herdr pane mod is gone (one-time notice)\n")
	b.WriteString("  ------------------------------------------------------\n")
	b.WriteString("  `wipnote delegate run` does its job now. wipnote did not move your settings.\n")
	fmt.Fprintf(&b, "  Claude Code still holds options for %q in %s:\n", key, path)
	mapped := map[string]json.RawMessage{}
	for _, opt := range retiredModOptions {
		v, ok := options[opt.key]
		if !ok {
			continue
		}
		where := opt.gone
		if where == "" {
			where = "delegate." + opt.key
			mapped[opt.key] = v
		}
		fmt.Fprintf(&b, "    %s = %s  ->  %s\n", opt.key, string(v), where)
	}
	if len(mapped) > 0 {
		snippet, _ := json.Marshal(map[string]any{"delegate": mapped})
		b.WriteString("  To keep them, add this to ~/.wipnote/config.json (merge with what is there):\n")
		fmt.Fprintf(&b, "    %s\n", snippet)
	}
	b.WriteString("  Then remove that pluginConfigs entry. This notice shows once.\n\n")
	return b.String()
}
