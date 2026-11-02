package main

import "testing"

func TestSpawnCmd_SetupFlags(t *testing.T) {
	cmd := delegateSpawnCmd()
	for _, f := range []string{"profile", "tool", "mcp", "plugin", "user-rules", "model", "effort", "cache-ttl"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("spawn has no --%s", f)
		}
	}
	if cmd.Flags().Lookup("full") != nil {
		t.Error("--full is replaced by --profile full")
	}
}
