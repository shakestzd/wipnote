package paths

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// Layered config, as Claude Code layers its settings. Low to high:
//
//	user     ~/.wipnote/config.json            per user, every project
//	project  <project>/.wipnote/config.json    committed, shared
//	local    <project>/.wipnote/config.local.json  per user and project, gitignored
//
// Environment variables beat every file; the caller applies them. The local
// and user files are read whether or not .wipnote/ is committed.

// LocalConfigPath is the per-user project config,
// <projectDir>/.wipnote/config.local.json. The managed .wipnote/.gitignore
// ignores it.
func LocalConfigPath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", "config.local.json")
}

// UserConfigPath is ~/.wipnote/config.json ($HOME honoured), or "" when
// there is no home directory.
func UserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".wipnote", "config.json")
}

// LayeredOptions say which object of the config files to merge and which of
// its keys hold host paths.
type LayeredOptions struct {
	Section string // the top-level object merged per key, e.g. "delegate"
	// PathKeys are host-path keys: accepted only from the user and local
	// files. In the committed project file they are ignored with a warning.
	PathKeys []string
	Warn     io.Writer // one line per ignored key; nil discards
}

// ReadLayeredConfig merges o.Section from the user, project and local files
// key by key (a higher layer's key replaces a lower one's) and decodes the
// result into v. A missing or malformed file is skipped. It reports whether
// any layer held the section.
func ReadLayeredConfig(projectDir string, o LayeredOptions, v any) bool {
	merged := map[string]json.RawMessage{}
	found := false
	for _, l := range []struct {
		path    string
		project bool
	}{{UserConfigPath(), false}, {ProjectConfigPath(projectDir), true}, {LocalConfigPath(projectDir), false}} {
		keys, ok := readSection(l.path, o.Section)
		if !ok {
			continue
		}
		found = true
		for k, raw := range keys {
			if l.project && slices.Contains(o.PathKeys, k) {
				warnPathKey(o, k)
				continue
			}
			merged[k] = raw
		}
	}
	data, _ := json.Marshal(merged)
	_ = json.Unmarshal(data, v)
	return found
}

// readSection returns the keys of one file's section; ok is false when the
// file is missing, malformed or has no such object.
func readSection(path, section string) (map[string]json.RawMessage, bool) {
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return nil, false
	}
	var keys map[string]json.RawMessage
	if raw, ok := top[section]; !ok || json.Unmarshal(raw, &keys) != nil || keys == nil {
		return nil, false
	}
	return keys, true
}

func warnPathKey(o LayeredOptions, key string) {
	if o.Warn == nil {
		return
	}
	fmt.Fprintf(o.Warn, "wipnote: ignoring %q in .wipnote/config.json (committed); a host path belongs in ~/.wipnote/config.json or .wipnote/config.local.json\n",
		o.Section+"."+key)
}
