package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseSinceDate_YYYYMMDD(t *testing.T) {
	got, err := parseSinceDate("2025-01-15")
	if err != nil {
		t.Fatalf("parseSinceDate: %v", err)
	}
	want := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseSinceDate_RFC3339(t *testing.T) {
	got, err := parseSinceDate("2025-03-01T12:00:00Z")
	if err != nil {
		t.Fatalf("parseSinceDate: %v", err)
	}
	if got.Year() != 2025 || got.Month() != 3 || got.Day() != 1 {
		t.Errorf("unexpected date: %v", got)
	}
}

func TestParseSinceDate_Invalid(t *testing.T) {
	_, err := parseSinceDate("not-a-date")
	if err == nil {
		t.Error("expected error for invalid date, got nil")
	}
}

func TestBlameCmd_InvalidFormat(t *testing.T) {
	err := runBlame("any/path.go", blameOpts{format: "yaml"})
	if err == nil {
		t.Error("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error message should mention 'unknown format', got: %v", err)
	}
}

func TestBlameCmd_BadSinceDate(t *testing.T) {
	err := runBlame("any/path.go", blameOpts{format: "text", since: "not-a-date"})
	if err == nil {
		t.Error("expected error for bad --since date")
	}
	if !strings.Contains(err.Error(), "--since") {
		t.Errorf("error should mention --since, got: %v", err)
	}
}
