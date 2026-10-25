package main

import (
	"encoding/json"
	"os"
)

// marketplacePluginRegistryScopes lists all registry keys that
// removeMarketplaceWipnote handles. Detection scope must match removal scope
// so marketplaceWipnotePresent() doesn't miss registry-only installs that
// lack artifact directories (e.g. wipnote@local-marketplace or legacy
// htmlgraph scopes).
var marketplacePluginRegistryScopes = []string{
	"wipnote@wipnote",
	"wipnote@local-marketplace",
	"htmlgraph@htmlgraph",
	"htmlgraph@local-marketplace",
}

// isAnyMarketplacePluginInstalledAt returns true when installed_plugins.json
// at path contains a non-empty entry for ANY of the registry scopes that
// removeMarketplaceWipnote handles. Testable core used by
// marketplaceWipnotePresent.
func isAnyMarketplacePluginInstalledAt(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var outer struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(data, &outer) != nil {
		return false
	}
	for _, scope := range marketplacePluginRegistryScopes {
		raw, ok := outer.Plugins[scope]
		if !ok {
			continue
		}
		var entries []json.RawMessage
		if json.Unmarshal(raw, &entries) == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}
