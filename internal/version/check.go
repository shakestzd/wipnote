// Package version provides update-check utilities for the wipnote CLI.
package version

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	cacheFile   = "version-check.json"
	cacheMaxAge = 24 * time.Hour
)

// cache is the on-disk structure for the cached version check result.
type cache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

// CheckForUpdate compares the installed version against the latest available
// version. Returns (latestVersion, isNewer, error).
//
// Resolution chain (no network first):
//  1. Read from cache (~/.local/share/wipnote/version-check.json) if < 24h old.
//  2. Read ~/.claude/plugins/marketplaces/wipnote/plugin/.claude-plugin/plugin.json
//     — the local marketplace clone; no network needed.
//  3. Run `gh release view --json tagName -q .tagName` — requires gh CLI.
//  4. If all fail, return ("", false, nil) — silent degradation.
func CheckForUpdate(currentVersion string) (string, bool, error) {
	// Try cache first.
	if cached, ok := readCache(); ok {
		return cached, isNewer(cached, currentVersion), nil
	}

	// Try marketplace clone (no network).
	if latest := readMarketplaceVersion(); latest != "" {
		writeCache(latest)
		return latest, isNewer(latest, currentVersion), nil
	}

	// Try gh CLI (network).
	if latest := readGHRelease(); latest != "" {
		writeCache(latest)
		return latest, isNewer(latest, currentVersion), nil
	}

	return "", false, nil
}

// readCache reads the cached version if it exists and is fresh (< 24h old).
func readCache() (string, bool) {
	p := cachePath()
	if p == "" {
		return "", false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	var c cache
	if err := json.Unmarshal(data, &c); err != nil {
		return "", false
	}
	if c.Latest == "" || time.Since(c.CheckedAt) > cacheMaxAge {
		return "", false
	}
	return c.Latest, true
}

// writeCache persists the latest version to disk. Errors are silently ignored.
func writeCache(latest string) {
	p := cachePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	c := cache{Latest: latest, CheckedAt: time.Now().UTC()}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}

// cachePath returns ~/.local/share/wipnote/version-check.json.
// Returns "" if the home directory cannot be determined.
func cachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "wipnote", cacheFile)
}

// readMarketplaceVersion reads the version from the local marketplace clone.
// Returns "" on any error.
func readMarketplaceVersion() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	pluginJSON := filepath.Join(home, ".claude", "plugins", "marketplaces", "wipnote", "plugin", ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginJSON)
	if err != nil {
		return ""
	}
	var p struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(p.Version), "v")
}

// readGHRelease runs `gh release view --json tagName -q .tagName`.
// Returns "" on any error (gh not installed, no network, etc.).
func readGHRelease() string {
	cmd := exec.Command("gh", "release", "view", "--json", "tagName", "-q", ".tagName")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	tag := strings.TrimSpace(string(out))
	return strings.TrimPrefix(tag, "v")
}

// isNewer reports whether latest is strictly newer than current using
// semantic version comparison (major.minor.patch). Both versions may
// optionally carry a leading "v". Non-parseable segments are treated as 0.
func isNewer(latest, current string) bool {
	lp := parseSemver(latest)
	cp := parseSemver(current)
	for i := range lp {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

// parseSemver parses a version string like "1.2.3" or "v1.2.3" into a
// three-element [major, minor, patch] slice. Missing or non-numeric
// components default to 0.
func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(p)
		result[i] = n
	}
	return result
}
