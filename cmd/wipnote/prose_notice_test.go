package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteProseAdvice_CapsAtFive(t *testing.T) {
	var buf bytes.Buffer
	text := strings.Repeat("Do this; then that. ", 8)
	writeProseAdvice(&buf, []proseItem{{path: "description", text: text}})
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != maxProseNotes+1 {
		t.Fatalf("want %d lines, got %d:\n%s", maxProseNotes+1, len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "prose description:1:") || !strings.Contains(lines[0], "semicolon") {
		t.Errorf("first line: %q", lines[0])
	}
	if want := "… and 3 more (wipnote prose check)"; !strings.Contains(lines[5], want) {
		t.Errorf("last line %q lacks %q", lines[5], want)
	}
}

func TestWriteProseAdvice_CleanTextPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	writeProseAdvice(&buf, []proseItem{{path: "why", text: "Run the gate."}})
	if buf.Len() != 0 {
		t.Errorf("want no output, got %q", buf.String())
	}
}

func TestProseSilenced_Off(t *testing.T) {
	t.Setenv("WIPNOTE_PROSE", "off")
	if !proseSilenced() {
		t.Error("WIPNOTE_PROSE=off should silence notes")
	}
}

func TestProseCheckCmd_StrictExit(t *testing.T) {
	var out bytes.Buffer
	n, err := runProseCheck("-", strings.NewReader("Bad; text."), &out, "text")
	if err != nil || n != 1 || !strings.HasPrefix(out.String(), "-:1:4  semicolon  ") {
		t.Fatalf("n=%d err=%v out=%q", n, err, out.String())
	}
	out.Reset()
	if _, err := runProseCheck("-", strings.NewReader("Fine."), &out, "json"); err != nil || strings.TrimSpace(out.String()) != "[]" {
		t.Errorf("json clean: %q %v", out.String(), err)
	}
}
