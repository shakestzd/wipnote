package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// defaultAutoCompactWindow is the --autocompact value wipnote passes to Claude
// Code when nothing else says otherwise (feat-3fcabee6). Why: Claude Code
// compacts 1M-context models at about 967K by default, cost per turn scales
// with context, and compaction lands near 100K, so 300K leaves about 200K of
// working room.
const defaultAutoCompactWindow = "300k"

const (
	autoCompactMinTokens = 100_000
	autoCompactMaxTokens = 1_000_000
)

// autoCompactEnvVars are Claude Code's own switches for compaction; any of
// them set means the user has chosen, and wipnote adds nothing.
var autoCompactEnvVars = []string{
	"CLAUDE_CODE_AUTO_COMPACT_WINDOW", "DISABLE_AUTO_COMPACT", "DISABLE_COMPACT",
}

var autoCompactTokens = regexp.MustCompile(`^([0-9]+)([km]?)$`)

// withAutoCompact returns extra with --autocompact <window> prepended, or extra
// unchanged when the flag must not be added: the user passed --autocompact, an
// env var in autoCompactEnvVars is set, or the config says auto/off.
// cfgPath is ~/.wipnote/config.json; launch.claude.autoCompactWindow in it is
// unset (default), "auto"/"off" (no flag) or a token count from 100k to 1M
// ("400k", 400000). An invalid value warns once on warn and uses the default.
func withAutoCompact(extra []string, getenv func(string) string, cfgPath string, warn io.Writer) []string {
	if hasAutoCompactFlag(extra) {
		return extra
	}
	for _, k := range autoCompactEnvVars {
		if getenv(k) != "" {
			return extra
		}
	}
	window, ok := resolveAutoCompactWindow(cfgPath, warn)
	if !ok {
		return extra
	}
	return append([]string{"--autocompact", window}, extra...)
}

func hasAutoCompactFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--autocompact" || strings.HasPrefix(a, "--autocompact=") {
			return true
		}
	}
	return false
}

// resolveAutoCompactWindow reads launch.claude.autoCompactWindow from the user
// config. ok is false when the config turns the flag off.
func resolveAutoCompactWindow(cfgPath string, warn io.Writer) (window string, ok bool) {
	raw := readAutoCompactSetting(cfgPath)
	if raw == nil {
		return defaultAutoCompactWindow, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return invalidAutoCompact(warn, string(raw))
		}
		s = n.String()
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "auto" || s == "off" {
		return "", false
	}
	if validAutoCompactTokens(s) {
		return s, true
	}
	return invalidAutoCompact(warn, s)
}

func invalidAutoCompact(warn io.Writer, v string) (string, bool) {
	fmt.Fprintf(warn, "wipnote: ignoring launch.claude.autoCompactWindow %q in ~/.wipnote/config.json (want auto, off, or 100k to 1m tokens); using %s\n", v, defaultAutoCompactWindow)
	return defaultAutoCompactWindow, true
}

func validAutoCompactTokens(s string) bool {
	m := autoCompactTokens.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1])
	// Reject before scaling: a count above the maximum is invalid with any
	// suffix, and scaling a huge one can wrap back into range (bug-58913cab).
	if err != nil || n > autoCompactMaxTokens {
		return false
	}
	switch m[2] {
	case "k":
		n *= 1000
	case "m":
		n *= 1_000_000
	}
	return n >= autoCompactMinTokens && n <= autoCompactMaxTokens
}

// readAutoCompactSetting returns the raw launch.claude.autoCompactWindow, or
// nil when the file, the key or valid JSON is missing. Like
// delegate.claude.profiles it is read from the user config file only.
func readAutoCompactSetting(path string) json.RawMessage {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f struct {
		Launch struct {
			Claude struct {
				Window json.RawMessage `json:"autoCompactWindow"`
			} `json:"claude"`
		} `json:"launch"`
	}
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	return f.Launch.Claude.Window
}
