package paths

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"reflect"
)

// packageJSONDependencyKeys are the package.json fields that choose which
// third-party packages get installed. A change to any of them is a dependency
// change; scripts, name, version and the other fields are not (GitHub #334).
// overrides and resolutions are included because they change resolved
// versions without touching the dependency blocks.
var packageJSONDependencyKeys = []string{
	"dependencies",
	"devDependencies",
	"peerDependencies",
	"optionalDependencies",
	"bundleDependencies",
	"bundledDependencies",
	"overrides",
	"resolutions",
}

// PackageJSONDependenciesChanged reports whether two versions of a
// package.json differ in what they install. An empty version is a missing
// file. Either version failing to parse counts as a change, so the research
// gate stays on when the answer is unknown.
func PackageJSONDependenciesChanged(before, after []byte) bool {
	b, ok := packageJSONDependencies(before)
	if !ok {
		return true
	}
	a, ok := packageJSONDependencies(after)
	if !ok {
		return true
	}
	return !reflect.DeepEqual(b, a)
}

// packageJSONDependencies decodes the dependency fields of a package.json.
// Missing or empty content is an empty set; content that does not parse as a
// JSON object reports false.
func packageJSONDependencies(content []byte) (map[string]any, bool) {
	out := map[string]any{}
	if len(bytes.TrimSpace(content)) == 0 {
		return out, true
	}
	var doc map[string]any
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, false
	}
	for _, k := range packageJSONDependencyKeys {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	return out, true
}

// GitShow returns the content `git show <spec>` prints in root, where spec is
// "<rev>:<path>" or ":<path>" for the index. A path absent there (a new or
// deleted file, a root commit's parent, a repo with no HEAD) is empty content
// and ok; any other git failure is not ok, so a caller can fail safe.
func GitShow(root, spec string) ([]byte, bool) {
	cmd := exec.Command("git", "-C", root, "show", spec)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, true
	}
	for _, absent := range []string{"does not exist in", "exists on disk, but not in", "invalid object name", "unknown revision"} {
		if bytes.Contains(stderr.Bytes(), []byte(absent)) {
			return nil, true
		}
	}
	return nil, false
}
