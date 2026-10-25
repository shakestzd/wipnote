package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const listTestLongTitle = "A feature title that is much longer than the forty-four characters the table keeps"

func writeListFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	features := filepath.Join(dir, "features")
	if err := os.MkdirAll(features, 0o755); err != nil {
		t.Fatal(err)
	}
	items := map[string]string{
		"feat-aaaa1111": `<article id="feat-aaaa1111" data-type="feature" data-status="in-progress" data-priority="high" data-track-id="trk-12345678" data-updated="2026-10-24T10:00:00Z"><header><h1>` + listTestLongTitle + `</h1></header></article>`,
		"feat-bbbb2222": `<article id="feat-bbbb2222" data-type="feature" data-status="todo" data-priority="low"><header><h1>Short title</h1></header></article>`,
	}
	for id, body := range items {
		if err := os.WriteFile(filepath.Join(features, id+".html"), []byte("<html><body>"+body+"</body></html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestWiList_JSONKeepsFullTitles(t *testing.T) {
	dir := writeListFixture(t)
	var buf bytes.Buffer
	if err := runWiListTo(&buf, dir, "features", "", "json"); err != nil {
		t.Fatalf("runWiListTo: %v", err)
	}
	var rows []wiListRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(rows), rows)
	}
	got := rows[0]
	if got.ID != "feat-aaaa1111" || got.Title != listTestLongTitle || got.Status != "in-progress" ||
		got.Priority != "high" || got.TrackID != "trk-12345678" || got.Type != "feature" {
		t.Errorf("row 0 = %+v", got)
	}
}

func TestWiList_JSONStatusFilterAndEmpty(t *testing.T) {
	dir := writeListFixture(t)
	var buf bytes.Buffer
	if err := runWiListTo(&buf, dir, "features", "done", "json"); err != nil {
		t.Fatalf("runWiListTo: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty JSON list = %q, want []", buf.String())
	}
}

func TestWiList_TextStillTruncates(t *testing.T) {
	dir := writeListFixture(t)
	var buf bytes.Buffer
	if err := runWiListTo(&buf, dir, "features", "", "text"); err != nil {
		t.Fatalf("runWiListTo: %v", err)
	}
	if strings.Contains(buf.String(), listTestLongTitle) {
		t.Error("text table should truncate long titles")
	}
	if !strings.Contains(buf.String(), "2 features") {
		t.Errorf("text table footer missing: %q", buf.String())
	}
}

func TestWiList_RejectsUnknownFormat(t *testing.T) {
	dir := writeListFixture(t)
	if err := runWiListTo(&bytes.Buffer{}, dir, "features", "", "yaml"); err == nil {
		t.Fatal("want an error for an unknown format")
	}
}
