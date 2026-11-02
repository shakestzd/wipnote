package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrBadSetup refuses a profile, MCP server, plugin or flag the worker's
// setup cannot honor.
var ErrBadSetup = errors.New("delegate: bad worker setup")

// Lean Claude workers start with these built-in tools and nothing else the
// user's setup would add. Measured on real workers (first request, cache write
// tokens): 75,100 with Claude's defaults, 15,134 lean.
var leanTools = []string{"Bash", "Read", "Edit", "Write"}

// hasFlag reports whether args hold flag, bare or as flag=value.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// LeanSpec is what the lean flags are built from; the zero value is the
// bare lean worker.
type LeanSpec struct {
	Home      string          // for the CLAUDE.md exclude; "" omits it
	UserRules bool            // keep ~/.claude/CLAUDE.md
	Tools     []string        // built-in tools beyond Bash, Read, Edit, Write
	Plugins   map[string]bool // enabledPlugins overlay (name@marketplace)
	Deny      []string        // MCP servers to deny by exact name
	Open      bool            // drop --strict-mcp-config (connector or plugin servers)
	MCP       bool            // any MCP server is on: adds ToolSearch
	CacheTTL  string          // prompt cache lifetime, "5m" (default) or "1h"
}

// Prompt cache lifetimes a worker can take. A subscription main conversation
// writes the cache at the 1h rate (2x input) and 5m costs 1.25x; a short
// worker never uses the hour, so 5m is the default.
const (
	CacheTTL5m = "5m"
	CacheTTL1h = "1h"
)

// checkCacheTTL refuses any lifetime but "", 5m or 1h.
func checkCacheTTL(v string) error {
	if v == "" || v == CacheTTL5m || v == CacheTTL1h {
		return nil
	}
	return fmt.Errorf("%w: cache TTL %q; valid: %s, %s", ErrBadSetup, v, CacheTTL5m, CacheTTL1h)
}

// toolList is the one comma-joined --tools value. ToolSearch rides with any
// MCP server (without it every tool definition loads in full: 159,656 tokens
// against 18,474) and Skill with any plugin (its skills are unreachable
// otherwise).
func (s LeanSpec) toolList() string {
	extra := append([]string{}, s.Tools...)
	if s.MCP {
		extra = append(extra, "ToolSearch")
	}
	if len(s.Plugins) > 0 {
		extra = append(extra, "Skill")
	}
	return strings.Join(unionNames(leanTools, extra), ",")
}

// settings is the --settings JSON. Project settings still load:
// --setting-sources project hangs a worker on a dialog nobody can answer.
func (s LeanSpec) settings() string {
	m := map[string]any{
		"autoMemoryEnabled":      false,
		"outputStyle":            "default",
		"syncClaudeAiPlugins":    false,
		"remoteControlAtStartup": false,
		"includeGitInstructions": false,
		"promptCacheTtl":         CacheTTL5m,
	}
	if s.CacheTTL != "" {
		m["promptCacheTtl"] = s.CacheTTL
	}
	if s.Home != "" && !s.UserRules {
		m["claudeMdExcludes"] = []string{strings.TrimRight(s.Home, "/") + "/.claude/CLAUDE.md"}
	}
	if len(s.Plugins) > 0 {
		m["enabledPlugins"] = s.Plugins
	}
	if len(s.Deny) > 0 {
		deny := make([]map[string]string, 0, len(s.Deny))
		for _, n := range s.Deny {
			deny = append(deny, map[string]string{"serverName": n})
		}
		m["deniedMcpServers"] = deny
	}
	data, _ := json.Marshal(m) // strings, bools and maps of them cannot fail
	return string(data)
}

// LeanFlags are the default flags of a Claude worker. A flag the user passed
// after -- (user) replaces its default; their --settings replaces the whole
// overlay, so the plugin and MCP deny entries go with it. They follow the
// brief, as BootArgv requires, and --tools stays one comma-joined value
// because the flag is variadic.
func LeanFlags(user []string, s LeanSpec) []string {
	var flags []string
	if !s.Open && !hasFlag(user, "--strict-mcp-config") {
		flags = append(flags, "--strict-mcp-config")
	}
	if !hasFlag(user, "--tools") {
		flags = append(flags, "--tools", s.toolList())
	}
	if !hasFlag(user, "--chrome") && !hasFlag(user, "--no-chrome") {
		flags = append(flags, "--no-chrome")
	}
	if !hasFlag(user, "--settings") {
		flags = append(flags, "--settings", s.settings())
	}
	return flags
}
