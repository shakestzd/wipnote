package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServeChild_NoIndexerStatusEndpoint asserts that the serve-child mux
// (the per-project HTTP server) does NOT expose /api/indexer/status. The
// NDJSON->SQLite indexer was removed from serve-child to avoid contention
// between two writer-pool sql.DB handles against the same WAL file
// (bug-28a9d7a7); resurrecting the endpoint without the indexer would
// crash on a nil receiver, and resurrecting the indexer would re-introduce
// the SQLITE_BUSY storms. This test fails fast if either happens by
// accident.
func TestServeChild_NoIndexerStatusEndpoint(t *testing.T) {
	mux := buildSingleProjectMux(nil, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/indexer/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("/api/indexer/status returned %d, want 404 — indexer endpoint must remain removed", rec.Code)
	}
}
