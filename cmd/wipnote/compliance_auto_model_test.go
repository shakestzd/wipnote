package main

import "testing"

func TestComplianceAuto_DefaultModelIsAlias(t *testing.T) {
	f := complianceAutoCmd().Flags().Lookup("model")
	if f == nil {
		t.Fatal("--model flag missing")
	}
	if f.DefValue != "sonnet" {
		t.Errorf("default --model = %q, want alias %q", f.DefValue, "sonnet")
	}
}
