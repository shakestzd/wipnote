package session

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ProfileFull is Claude's own start: no lean flags, the user's model and
// effort, and the orchestrator prompt.
const ProfileFull = "full"

// DefaultProfile is used when neither --profile nor the user config names one.
const DefaultProfile = "standard"

// Profile is a named worker setup. Flags add to or override it.
type Profile struct {
	Model     string   `json:"model,omitempty"`
	Effort    string   `json:"effort,omitempty"`
	Tools     []string `json:"tools,omitempty"`
	MCP       []string `json:"mcp,omitempty"`
	Plugins   []string `json:"plugins,omitempty"`
	UserRules bool     `json:"userRules,omitempty"`
	CacheTTL  string   `json:"cacheTTL,omitempty"`
}

// builtinProfiles are the profiles every install has. full is special: it
// turns the lean flags off (see Setup.Full).
var builtinProfiles = map[string]Profile{
	"simple":    {Model: "haiku", Effort: "low"},
	"standard":  {Model: "sonnet", Effort: "medium"},
	"deep":      {Model: "opus", Effort: "high"},
	ProfileFull: {},
}

// ClaudeConfig is delegate.claude in ~/.wipnote/config.json.
type ClaudeConfig struct {
	DefaultProfile string             `json:"defaultProfile"`
	Profiles       map[string]Profile `json:"profiles"`
}

// LoadClaudeConfig reads delegate.claude from a wipnote config file. A missing
// or malformed file is an empty config: the built-in profiles apply.
func LoadClaudeConfig(path string) ClaudeConfig {
	var f struct {
		Delegate struct {
			Claude ClaudeConfig `json:"claude"`
		} `json:"delegate"`
	}
	if data, err := os.ReadFile(path); path != "" && err == nil {
		_ = json.Unmarshal(data, &f)
	}
	return f.Delegate.Claude
}

// ProfileNames lists every profile name, sorted.
func (c ClaudeConfig) ProfileNames() []string {
	seen := map[string]bool{}
	for n := range builtinProfiles {
		seen[n] = true
	}
	for n := range c.Profiles {
		seen[n] = true
	}
	return sortedKeys(seen)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Resolve returns the named profile (the config default, then standard, when
// name is empty); a user profile overrides a built-in of the same name. An
// unknown name is refused with the valid ones listed.
func (c ClaudeConfig) Resolve(name string) (string, Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		name = DefaultProfile
	}
	if p, ok := c.Profiles[name]; ok {
		return name, p, nil
	}
	if p, ok := builtinProfiles[name]; ok {
		return name, p, nil
	}
	return "", Profile{}, fmt.Errorf("%w: unknown profile %q; valid: %s", ErrBadSetup, name, strings.Join(c.ProfileNames(), ", "))
}

// Setup is what a Claude worker was given, as spawn reports and records it.
type Setup struct {
	Profile   string   `json:"profile,omitempty"`
	Model     string   `json:"model,omitempty"`
	Effort    string   `json:"effort,omitempty"`
	Tools     []string `json:"tools,omitempty"`
	MCP       []string `json:"mcp,omitempty"`
	Plugins   []string `json:"plugins,omitempty"`
	UserRules bool     `json:"user_rules,omitempty"`
	CacheTTL  string   `json:"cache_ttl,omitempty"`
}

// String is the ledger's setup column: names only.
func (s Setup) String() string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("profile", s.Profile)
	add("effort", s.Effort)
	add("tools", strings.Join(s.Tools, ","))
	add("mcp", strings.Join(s.MCP, ","))
	add("plugins", strings.Join(s.Plugins, ","))
	add("cache-ttl", s.CacheTTL)
	if s.UserRules {
		add("user-rules", "yes")
	}
	return strings.Join(parts, ";")
}

// setup is the Setup a spawn chose.
func (o SpawnOptions) setup() Setup {
	return Setup{Profile: o.Profile, Model: o.Model, Effort: o.Effort, Tools: o.Tools, MCP: o.MCP,
		Plugins: o.Plugins, UserRules: o.UserRules, CacheTTL: o.CacheTTL}
}

// applyProfile merges a profile with a spawn's flags into the options harnessFlags
// reads. A model or effort the user passed after -- (raw) wins over the profile,
// and --model/--effort win over both. Lists add; --user-rules ORs.
func applyProfile(o *SpawnOptions, name string, p Profile) {
	o.Profile = name
	o.Full = name == ProfileFull
	if o.Model == "" && FlagValue(o.Extra) == "" {
		o.Model = p.Model
	}
	if o.Effort == "" && !hasFlag(o.Extra, "--effort") {
		o.Effort = p.Effort
	}
	o.Tools = unionNames(p.Tools, o.Tools)
	o.MCP = unionNames(p.MCP, o.MCP)
	o.Plugins = unionNames(p.Plugins, o.Plugins)
	o.UserRules = p.UserRules || o.UserRules
	if o.CacheTTL == "" {
		o.CacheTTL = p.CacheTTL
	}
}

// unionNames joins lists in order, dropping repeats.
func unionNames(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range lists {
		for _, n := range l {
			if n = strings.TrimSpace(n); n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// checkSetup refuses what a Codex worker cannot take.
func checkSetup(o SpawnOptions) error {
	if o.Harness != HarnessCodex {
		return nil
	}
	if (o.Profile != "" && o.Profile != DefaultProfile) || len(o.Tools)+len(o.MCP)+len(o.Plugins) > 0 || o.UserRules || o.CacheTTL != "" {
		return fmt.Errorf("%w: --profile, --tool, --mcp, --plugin, --user-rules and --cache-ttl are for Claude workers; --model and --effort work on Codex", ErrBadSetup)
	}
	return nil
}

// choseSetup reports that the spawn asked for capabilities itself.
func (o SpawnOptions) choseSetup() bool {
	return len(o.Tools)+len(o.MCP)+len(o.Plugins) > 0 || o.UserRules || o.CacheTTL != ""
}
