package main

import (
	"strings"
	"testing"
)

func TestBuildGeminiOtelEnv_PortZeroReturnsBase(t *testing.T) {
	base := []string{"FOO=bar", "BAZ=qux"}
	got := buildGeminiOtelEnv(base, 0, "sess-123")
	if len(got) != len(base) {
		t.Fatalf("expected len %d, got %d: %v", len(base), len(got), got)
	}
	for i, v := range base {
		if got[i] != v {
			t.Errorf("entry %d: expected %q, got %q", i, v, got[i])
		}
	}
}

func TestBuildGeminiOtelEnv_SetsAllFourVars(t *testing.T) {
	got := buildGeminiOtelEnv(nil, 4317, "sess-abc")
	want := map[string]string{
		"GEMINI_TELEMETRY_ENABLED":       "true",
		"GEMINI_TELEMETRY_USE_COLLECTOR": "true",
		"GEMINI_TELEMETRY_OTLP_ENDPOINT": "http://127.0.0.1:4317",
		"GEMINI_TELEMETRY_OTLP_PROTOCOL": "http",
		"HTMLGRAPH_OTEL_SESSION":         "sess-abc",
	}
	for key, wantVal := range want {
		found := false
		for _, e := range got {
			if strings.HasPrefix(e, key+"=") {
				gotVal := strings.TrimPrefix(e, key+"=")
				if gotVal != wantVal {
					t.Errorf("%s: expected %q, got %q", key, wantVal, gotVal)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("env var %s not set; env=%v", key, got)
		}
	}
}

func TestBuildGeminiOtelEnv_OverridesExisting(t *testing.T) {
	base := []string{
		"GEMINI_TELEMETRY_ENABLED=false",
		"OTHER=val",
	}
	got := buildGeminiOtelEnv(base, 9999, "sess-xyz")
	for _, e := range got {
		if e == "GEMINI_TELEMETRY_ENABLED=false" {
			t.Errorf("old GEMINI_TELEMETRY_ENABLED=false was not overridden; env=%v", got)
		}
	}
	found := false
	for _, e := range got {
		if e == "GEMINI_TELEMETRY_ENABLED=true" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GEMINI_TELEMETRY_ENABLED=true not found after override; env=%v", got)
	}
}
