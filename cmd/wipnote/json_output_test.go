package main

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

// TestWriteJSON_MatchesMarshalIndentPrintln pins that the shared helper
// prints the same bytes the MarshalIndent + Println copies printed: two-space
// indent, one trailing newline, HTML characters escaped, nil as null.
func TestWriteJSON_MatchesMarshalIndentPrintln(t *testing.T) {
	values := []any{
		map[string]any{"b": 1, "a": []string{"x", "<y>&"}},
		struct {
			ID   string `json:"id"`
			Tags []int  `json:"tags,omitempty"`
		}{ID: "feat-1"},
		[]string{},
		nil,
		"plain",
	}
	for _, v := range values {
		var buf bytes.Buffer
		if err := writeJSON(&buf, v); err != nil {
			t.Fatalf("writeJSON(%v): %v", v, err)
		}
		want, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != string(want)+"\n" {
			t.Errorf("writeJSON(%v) = %q, want %q", v, got, string(want)+"\n")
		}
	}
}

// TestWriteJSON_ErrorWritesNothing pins the failure side: a value JSON
// cannot encode returns an error and leaves the writer untouched.
func TestWriteJSON_ErrorWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, map[string]float64{"x": math.NaN()}); err == nil {
		t.Fatal("writeJSON(NaN) returned no error")
	}
	if buf.Len() != 0 {
		t.Errorf("writeJSON wrote %q on error", buf.String())
	}
}
