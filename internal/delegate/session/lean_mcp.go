package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// listMCP returns every MCP server name `claude mcp list` shows, run from the
// project root. It is a variable so tests need no claude.
var listMCP = claudeMCPList

// mcpPlan is what --mcp resolves to.
type mcpPlan struct {
	Servers map[string]json.RawMessage // written to the run's mcp.json (mode 0600)
	Deny    []string                   // deniedMcpServers, by exact name
	Open    bool                       // connector or plugin servers: no --strict-mcp-config
}

func isConnector(name string) bool {
	return strings.HasPrefix(name, "claude.ai ") || strings.HasPrefix(name, "plugin:")
}

// localMCP gathers the user's local servers: ~/.claude.json mcpServers and
// projects[root].mcpServers, then root/.mcp.json. The entries can hold
// secrets, so they only ever go into the 0600 file.
func localMCP(root string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	merge := func(data []byte, project string) {
		var f struct {
			Servers  map[string]json.RawMessage `json:"mcpServers"`
			Projects map[string]struct {
				Servers map[string]json.RawMessage `json:"mcpServers"`
			} `json:"projects"`
		}
		if json.Unmarshal(data, &f) != nil {
			return
		}
		for n, v := range f.Servers {
			out[n] = v
		}
		for n, v := range f.Projects[project].Servers {
			out[n] = v
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".claude.json")); err == nil {
			merge(data, root)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, ".mcp.json")); err == nil {
		merge(data, "")
	}
	return out
}

// resolveMCP turns --mcp names into a plan, or refuses with the valid names.
// Local servers stay under --strict-mcp-config with a generated config that
// holds only them. A claude.ai connector or plugin server cannot be loaded
// that way, so strict is dropped and every other server is denied by name
// (allowedMcpServers does not work: naming one connector removes all).
func resolveMCP(ctx context.Context, names []string, root string) (mcpPlan, error) {
	if len(names) == 0 {
		return mcpPlan{}, nil
	}
	local := localMCP(root)
	want, conn := map[string]bool{}, false
	for _, n := range names {
		if _, ok := local[n]; !ok && !isConnector(n) {
			return mcpPlan{}, fmt.Errorf("%w: unknown MCP server %q; local servers: %s (claude.ai connectors and plugin servers are named as `claude mcp list` shows them)",
				ErrBadSetup, n, strings.Join(sortedKeys(local), ", "))
		}
		want[n] = true
		conn = conn || isConnector(n)
	}
	if !conn {
		plan := mcpPlan{Servers: map[string]json.RawMessage{}}
		for n := range want {
			plan.Servers[n] = local[n]
		}
		return plan, nil
	}
	all, err := listMCP(ctx, root)
	if err != nil {
		return mcpPlan{}, fmt.Errorf("%w: cannot list MCP servers (claude mcp list): %v", ErrBadSetup, err)
	}
	known := map[string]bool{}
	plan := mcpPlan{Open: true}
	for _, n := range all {
		known[n] = true
		if !want[n] {
			plan.Deny = append(plan.Deny, n)
		}
	}
	for n := range want {
		if isConnector(n) && !known[n] {
			return mcpPlan{}, fmt.Errorf("%w: unknown MCP server %q; claude mcp list shows: %s", ErrBadSetup, n, strings.Join(sortedList(all), ", "))
		}
	}
	return plan, nil
}

func sortedList(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

// claudeMCPList runs `claude mcp list` (read-only) and returns the names. Its
// lines are "<name>: <url or command> - <status>" under a "Checking MCP server
// health…" heading; a name may hold spaces ("claude.ai Notion") and colons
// ("plugin:x:y"), so the name ends at the first ": ". Nothing after the name
// is kept, because it holds URLs and commands.
func claudeMCPList(ctx context.Context, root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "mcp", "list")
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude mcp list failed")
	}
	return parseMCPList(out.String()), nil
}

func parseMCPList(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || line[0] == ' ' || strings.HasPrefix(line, "Checking MCP") {
			continue
		}
		if i := strings.Index(line, ": "); i > 0 {
			names = append(names, line[:i])
		}
	}
	return names
}

// resolvePlugins returns the enabledPlugins overlay for --plugin names: every
// plugin the user has false except the requested ones (an entry only overlays,
// so unlisted plugins would stay on). A short name must match one plugin.
func resolvePlugins(names []string) (map[string]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	home, _ := os.UserHomeDir()
	var f struct {
		Enabled map[string]bool `json:"enabledPlugins"`
	}
	if data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	overlay := map[string]bool{}
	for k := range f.Enabled {
		overlay[k] = false
	}
	for _, n := range names {
		key, err := matchPlugin(n, f.Enabled)
		if err != nil {
			return nil, err
		}
		overlay[key] = true
	}
	return overlay, nil
}

func matchPlugin(name string, have map[string]bool) (string, error) {
	var found []string
	for k := range have {
		if k == name || strings.SplitN(k, "@", 2)[0] == name {
			found = append(found, k)
		}
	}
	valid := strings.Join(sortedKeys(have), ", ")
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", fmt.Errorf("%w: unknown plugin %q; valid: %s", ErrBadSetup, name, valid)
	}
	sort.Strings(found)
	return "", fmt.Errorf("%w: plugin %q is ambiguous (%s); use name@marketplace", ErrBadSetup, name, strings.Join(found, ", "))
}
