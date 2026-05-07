package main

import (
	"strings"
	"testing"
)

// TestSessionErrorMessages verifies error messages contain recovery guidance
func TestSessionErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		errorMsg    string
		mustContain []string
	}{
		{
			name:     "no active sessions error",
			errorMsg: "no active sessions found\nRun 'wipnote session start' to begin tracking, or specify a session ID explicitly.",
			mustContain: []string{
				"no active sessions found",
				"wipnote session start",
				"specify a session ID",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, phrase := range tt.mustContain {
				if !strings.Contains(tt.errorMsg, phrase) {
					t.Errorf("error message missing phrase: %q\nGot: %s", phrase, tt.errorMsg)
				}
			}
		})
	}
}

// TestClaimErrorMessages verifies error messages contain recovery guidance
func TestClaimErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		errorMsg    string
		mustContain []string
	}{
		{
			name:     "claim not found error",
			errorMsg: "claim \"clm-123\" not found — claims expire after 30 minutes of inactivity\nRun 'wipnote claim list' to see active claims.",
			mustContain: []string{
				"claim \"clm-123\" not found",
				"expire after 30 minutes",
				"wipnote claim list",
			},
		},
		{
			name:     "no active session for heartbeat",
			errorMsg: "no active session found — cannot auto-detect claim\nSpecify the claim ID directly: 'wipnote claim heartbeat clm-xxxxxxxx'. Run 'wipnote claim list' to find it.",
			mustContain: []string{
				"no active session found",
				"cannot auto-detect claim",
				"claim heartbeat clm-xxxxxxxx",
				"wipnote claim list",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, phrase := range tt.mustContain {
				if !strings.Contains(tt.errorMsg, phrase) {
					t.Errorf("error message missing phrase: %q\nGot: %s", phrase, tt.errorMsg)
				}
			}
		})
	}
}

// TestReportErrorMessages verifies error messages contain recovery guidance
func TestReportErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		errorMsg    string
		mustContain []string
	}{
		{
			name:     "no sessions found error",
			errorMsg: "no sessions found in the database\nRun 'wipnote ingest' to import Claude Code session transcripts, or 'wipnote session start' to begin tracking.",
			mustContain: []string{
				"no sessions found in the database",
				"wipnote ingest",
				"wipnote session start",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, phrase := range tt.mustContain {
				if !strings.Contains(tt.errorMsg, phrase) {
					t.Errorf("error message missing phrase: %q\nGot: %s", phrase, tt.errorMsg)
				}
			}
		})
	}
}
