package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

func TestCIStatusText_BaseAndRelease(t *testing.T) {
	published := time.Date(2026, 10, 20, 8, 30, 0, 0, time.UTC)
	four := 4
	s := &gitwatch.Snapshot{
		Available: true,
		PR:        &gitwatch.PR{Number: 7, Title: "Add thing", Branch: "x", Base: "main", State: "OPEN"},
		Base: &gitwatch.Base{Name: "main", State: gitwatch.BaseRed, RedToo: true,
			Failing: []gitwatch.BaseRun{{Workflow: "CI", Conclusion: "startup_failure", URL: "https://example.test/runs/22"}}},
		Release: &gitwatch.Release{State: gitwatch.ReleasePublished, Tag: "v1.4.0", PublishedAt: &published,
			CommitsSince: &four, Workflow: &gitwatch.ReleaseWorkflow{Name: "Publish", State: gitwatch.BaseGreen}},
		Notes: []string{"base: HTTP 403"},
	}
	var out bytes.Buffer
	writeCIStatusText(&out, s)
	writeBaseRelease(&out, s)
	writeNotes(&out, s)
	text := out.String()
	for _, want := range []string{
		"base main: red",
		"CI (startup_failure)  https://example.test/runs/22",
		"release v1.4.0 (2026-10-20), 4 commits since · Publish: green",
		"note: base: HTTP 403",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestCIStatusText_NoReleaseNoPR(t *testing.T) {
	s := &gitwatch.Snapshot{Available: true, Reason: "no pull request found",
		Base:    &gitwatch.Base{Name: "main", State: gitwatch.BaseNone},
		Release: &gitwatch.Release{State: gitwatch.ReleaseNone}}
	var out bytes.Buffer
	writeCIStatusText(&out, s)
	writeBaseRelease(&out, s)
	writeNotes(&out, s)
	text := out.String()
	for _, want := range []string{"no pull request found", "base main: none", "release: none"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}
