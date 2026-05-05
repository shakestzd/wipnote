package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTerminalGate_UnsetEnvReturns404 verifies that when ERINN_TERMINAL is
// not set the four /api/terminal/* routes are not registered and return 404.
func TestTerminalGate_UnsetEnvReturns404(t *testing.T) {
	// Ensure the env var is unset for this test.
	t.Setenv("ERINN_TERMINAL", "")

	mux := buildSingleProjectMux(nil, t.TempDir())

	endpoints := []string{
		"/api/terminal/sessions",
		"/api/terminal/start",
		"/api/terminal/stop",
		"/api/terminal/stop-all",
	}
	for _, path := range endpoints {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("ERINN_TERMINAL unset: %s returned %d, want 404", path, rec.Code)
		}
	}
}

// TestTerminalGate_SetEnvRegistersRoutes verifies that when ERINN_TERMINAL
// is set to exactly "1" the /api/terminal/sessions route is registered and does
// not return 404. (Other routes require a running ttyd process and are not
// probed here.)
func TestTerminalGate_SetEnvRegistersRoutes(t *testing.T) {
	t.Setenv("ERINN_TERMINAL", "1")

	mux := buildSingleProjectMux(nil, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/sessions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Errorf("ERINN_TERMINAL=1: /api/terminal/sessions returned 404, route should be registered")
	}
}

// TestTerminalGate_FalsyValuesReturn404 verifies that false-y or non-"1" values
// for ERINN_TERMINAL (notably "0" and "false") do NOT enable the routes.
// The contract is strict equality with "1"; any other value keeps the gate shut.
func TestTerminalGate_FalsyValuesReturn404(t *testing.T) {
	values := []string{"0", "false", "FALSE", "no", "off", "true", "yes"}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			t.Setenv("ERINN_TERMINAL", v)

			mux := buildSingleProjectMux(nil, t.TempDir())

			req := httptest.NewRequest(http.MethodGet, "/api/terminal/sessions", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("ERINN_TERMINAL=%q: /api/terminal/sessions returned %d, want 404 (only exact \"1\" enables)", v, rec.Code)
			}
		})
	}
}
