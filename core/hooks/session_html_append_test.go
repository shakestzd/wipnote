package hooks

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestAppendEventToSessionHTML_TailAppend pins feat-c30d0792: appending one
// event to a large session HTML reads only the file's tail, and the bytes on
// disk are exactly what the old read-modify-rewrite produced — the <li>
// inserted before the last </ol> with its indentation.
func TestAppendEventToSessionHTML_TailAppend(t *testing.T) {
	projectDir := t.TempDir()
	path := writeSessionHTMLFixture(t, projectDir, "tail-sess", 255*1024)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	onSessionHTMLRead = func(n int) { read += n }
	t.Cleanup(func() { onSessionHTMLRead = nil })

	ev := SessionEvent{
		Timestamp: time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC),
		ToolName:  "Edit", Success: false, EventID: "evt-<1>", FeatureID: "feat-x",
		Summary: `Edit: a "quoted" & <tagged> path`, Status: "aborted", Reason: "orphan",
	}
	AppendEventToSessionHTML(projectDir, "tail-sess", ev)

	li := `                <li data-ts="2026-10-25T13:00:00Z" data-tool="Edit" data-success="false"` +
		` data-event-id="evt-&lt;1&gt;" data-feature="feat-x" data-status="aborted" data-reason="orphan">` +
		`Edit: a &#34;quoted&#34; &amp; &lt;tagged&gt; path</li>` + "\n"
	idx := strings.LastIndex(string(before), "</ol>")
	want := string(before[:idx]) + li + "            " + string(before[idx:])
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("appended file differs from the full-rewrite result (len got %d, want %d)", len(got), len(want))
	}
	if read == 0 || read > 16*1024 {
		t.Errorf("append read %d bytes of a %d-byte file; want only the tail (0 < n <= 16 KiB)", read, len(before))
	}
}

// TestAppendEventToSessionHTML_MarkerOutsideTail covers a file whose last
// </ol> sits further from the end than the tail window: the append falls back
// to the full read and still inserts in the same place.
func TestAppendEventToSessionHTML_MarkerOutsideTail(t *testing.T) {
	projectDir := t.TempDir()
	path := writeSessionHTMLFixture(t, projectDir, "far-sess", 4*1024)
	before, _ := os.ReadFile(path)
	padded := strings.Replace(string(before), "</body>", "<!--"+strings.Repeat("p", 64*1024)+"--></body>", 1)
	if err := os.WriteFile(path, []byte(padded), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := benchSessionEvent(1)
	AppendEventToSessionHTML(projectDir, "far-sess", ev)
	got, _ := os.ReadFile(path)
	idx := strings.LastIndex(padded, "</ol>")
	if !strings.HasPrefix(string(got), padded[:idx]) || !strings.HasSuffix(string(got), "            "+padded[idx:]) {
		t.Fatalf("event not inserted before the last </ol>")
	}
	if !strings.Contains(string(got[idx:]), `data-event-id="evt-bench-1"`) {
		t.Errorf("appended <li> missing")
	}
}
