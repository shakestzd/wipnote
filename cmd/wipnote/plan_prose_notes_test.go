package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStreams runs fn with stdout and stderr replaced by pipes.
func captureStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	wo.Close()
	we.Close()
	o, _ := io.ReadAll(ro)
	e, _ := io.ReadAll(re)
	return string(o), string(e)
}

func TestPlanAddSliceYAML_PrintsProseNotesToStderrOnly(t *testing.T) {
	dir, id := briefPlan(t)
	var err error
	out, errOut := captureStreams(t, func() {
		err = runPlanAddSliceYAML(dir, id, "Delta", "Do this; then that.", "", "", "", "", "S", "Low", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "semicolon") || !strings.Contains(errOut, "slices[3].what") {
		t.Errorf("stderr = %q, want a semicolon note on slices[3].what", errOut)
	}
	if strings.Contains(out, "semicolon") || out != "Slice 4 added\n" {
		t.Errorf("stdout = %q, want only the slice line", out)
	}
}

func TestPlanCritiqueRevise_PrintsProseNotesAndHonoursOff(t *testing.T) {
	dir, id := briefPlan(t)
	var err error
	out, errOut := captureStreams(t, func() {
		err = runPlanCritiqueRevise(dir, id, 1, "Bad; text.", "", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "semicolon") || !strings.Contains(errOut, "slices[0].what") {
		t.Errorf("stderr = %q, want a semicolon note on slices[0].what", errOut)
	}
	if strings.Contains(out, "semicolon") {
		t.Errorf("stdout carries a note: %q", out)
	}
	t.Setenv("WIPNOTE_PROSE", "off")
	_, errOut = captureStreams(t, func() {
		err = runPlanCritiqueRevise(dir, id, 1, "Worse; text.", "", "")
	})
	if err != nil || errOut != "" {
		t.Errorf("WIPNOTE_PROSE=off: err=%v stderr=%q, want silence", err, errOut)
	}
}
