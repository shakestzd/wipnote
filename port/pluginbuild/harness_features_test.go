package pluginbuild

import (
	"os"
	"testing"
)

func TestAgentFrontmatterSpecs_OmitClaudeMdAutoCompactWindow(t *testing.T) {
	for _, field := range []string{"omitClaudeMd", "autoCompactWindow"} {
		var found *agentFrontmatterFieldSpec
		for i := range agentFrontmatterFieldSpecs {
			if agentFrontmatterFieldSpecs[i].Name == field {
				found = &agentFrontmatterFieldSpecs[i]
			}
		}
		if found == nil {
			t.Fatalf("spec %q missing", field)
		}
		if found.DocURL == "" && found.Provenance == "" {
			t.Errorf("%q lacks provenance", field)
		}
		if len(found.Harnesses) != 1 || found.Harnesses["claude"] != field {
			t.Errorf("%q must be claude-only, got %v", field, found.Harnesses)
		}
	}
}

func TestManifest_CompactHooksTargetCodex(t *testing.T) {
	wd, _ := os.Getwd()
	path, err := FindManifest(wd)
	if err != nil {
		t.Fatalf("manifest not found: %v", err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	seen := map[string]bool{}
	for _, ev := range m.Hooks.Events {
		if ev.Name != "PreCompact" && ev.Name != "PostCompact" {
			continue
		}
		// Claude keeps an async entry; Codex gets its own sync entry because
		// non-claude targets drop the async field.
		if ev.AppliesTo("codex") {
			seen[ev.Name] = true
		}
	}
	if !seen["PreCompact"] || !seen["PostCompact"] {
		t.Errorf("PreCompact/PostCompact need an entry targeting codex: %v", seen)
	}
}
