package pluginbuild

import "testing"

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
