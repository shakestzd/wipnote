package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shakestzd/wipnote/internal/childproc"
)

func TestIsValidProjectID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"", false},
		{".", false},
		{"..", false},
		{"a/b", false},
		{"a\\b", false},
		{"a\x00b", false},
		{"XYZ", false},     // not in [a-f0-9]
		{"abc", false},     // too short (< 4)
		{"deadbeef", true}, // canonical 8-char ID
		{"abcd1234", true}, // valid 8-char hex
		{"abc1", true},     // minimum 4-char
	}
	for _, tc := range tests {
		got := isValidProjectID(tc.id)
		if got != tc.want {
			t.Errorf("isValidProjectID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// TestProxyHandlerRejectsInvalidID checks the proxy returns 400 for
// malformed IDs without touching the supervisor or registry.
func TestProxyHandlerRejectsInvalidID(t *testing.T) {
	sup := childproc.NewSupervisor(childproc.Options{})
	h := proxyHandler(sup)

	bad := []string{
		"/p/",              // empty
		"/p/../etc/passwd", // traversal
		"/p/XYZ/api/stats", // non-hex
		"/p/ab/api/stats",  // too short
	}
	for _, p := range bad {
		req := httptest.NewRequest("GET", p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", p, rec.Code)
		}
	}
}

// TestProxyHandlerUnknownProject checks the proxy returns 404 for a
// well-formed but unregistered project ID.
func TestProxyHandlerUnknownProject(t *testing.T) {
	// Point the registry at a tmpdir so no real projects leak in.
	t.Setenv("HOME", t.TempDir())

	sup := childproc.NewSupervisor(childproc.Options{})
	h := proxyHandler(sup)

	req := httptest.NewRequest("GET", "/p/deadbeef/api/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: got %d, want 404", rec.Code)
	}
}
