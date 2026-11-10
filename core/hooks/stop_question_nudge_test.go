package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nudgeItem = `<article id="feat-aaaa0001" data-type="feature" data-status="in-progress" data-node-props="{&#34;questions&#34;:[{&#34;id&#34;:&#34;q-77&#34;,&#34;text&#34;:&#34;t&#34;,&#34;options&#34;:[],&#34;status&#34;:&#34;open&#34;}]}"><header><h1>T</h1></header></article>`

func nudgeProject(t *testing.T, withQuestion bool) string {
	t.Helper()
	root := t.TempDir()
	d := filepath.Join(root, ".wipnote", "features")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `<article id="feat-aaaa0001" data-type="feature"><header><h1>T</h1></header></article>`
	if withQuestion {
		body = nudgeItem
	}
	if err := os.WriteFile(filepath.Join(d, "feat-aaaa0001.html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestStopQuestionNudge(t *testing.T) {
	t.Setenv(DelegateRunEnv, "")
	root := nudgeProject(t, true)
	cases := []struct {
		name  string
		ev    CloudEvent
		env   string
		block bool
	}{
		{"prose question blocks", CloudEvent{LastAssistantMessage: "Done.\nShall I proceed?\n\n"}, "", true},
		{"no question mark", CloudEvent{LastAssistantMessage: "Done."}, "", false},
		{"question inside fence ignored", CloudEvent{LastAssistantMessage: "x\n```\nwhat?\n```"}, "", false},
		{"question before fence is not last line", CloudEvent{LastAssistantMessage: "ok?\n```\ncode\n```"}, "", false},
		{"stop_hook_active allows", CloudEvent{LastAssistantMessage: "ok?", StopHookActive: true}, "", false},
		{"worker allows", CloudEvent{LastAssistantMessage: "ok?"}, "dlg-0123456789ab", false},
		{"antigravity allows", CloudEvent{LastAssistantMessage: "ok?", Harness: HarnessAntigravity}, "", false},
		{"codex blocks", CloudEvent{LastAssistantMessage: "ok?", Harness: HarnessCodex}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(DelegateRunEnv, c.env)
			ev := c.ev
			res := stopQuestionNudge(&ev, root)
			if got := res != nil && res.Decision == "block"; got != c.block {
				t.Fatalf("block=%v want %v (%+v)", got, c.block, res)
			}
			if c.block && !strings.Contains(res.Reason, "q-77") {
				t.Fatalf("reason lacks id: %q", res.Reason)
			}
		})
	}
}

func TestStopQuestionNudge_NoOpenRecords(t *testing.T) {
	t.Setenv(DelegateRunEnv, "")
	root := nudgeProject(t, false)
	if res := stopQuestionNudge(&CloudEvent{LastAssistantMessage: "ok?"}, root); res != nil {
		t.Fatalf("blocked without records: %+v", res)
	}
}

// The nudge counts only open questions on in-progress items: an assumed
// question went ahead on purpose and a todo item is not being worked.
func TestStopQuestionNudge_OnlyOpenOnInProgress(t *testing.T) {
	t.Setenv(DelegateRunEnv, "")
	for name, body := range map[string]string{
		"assumed":   strings.Replace(nudgeItem, "&#34;open&#34;", "&#34;assumed&#34;", 1),
		"todo item": strings.Replace(nudgeItem, `data-status="in-progress"`, `data-status="todo"`, 1),
	} {
		root := nudgeProject(t, true)
		if err := os.WriteFile(filepath.Join(root, ".wipnote", "features", "feat-aaaa0001.html"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if res := stopQuestionNudge(&CloudEvent{LastAssistantMessage: "ok?"}, root); res != nil {
			t.Errorf("%s: blocked %+v", name, res)
		}
	}
}

// The reason lists at most five ids and counts the rest.
func TestStopQuestionNudge_ListsAtMostFive(t *testing.T) {
	t.Setenv(DelegateRunEnv, "")
	root := nudgeProject(t, false)
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("feat-aaaa%04d", i)
		body := strings.NewReplacer("feat-aaaa0001", id, "q-77", fmt.Sprintf("q-%02d", i)).Replace(nudgeItem)
		if err := os.WriteFile(filepath.Join(root, ".wipnote", "features", id+".html"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := stopQuestionNudge(&CloudEvent{LastAssistantMessage: "ok?"}, root)
	if res == nil || !strings.Contains(res.Reason, "and 2 more") || strings.Count(res.Reason, "q-0") != 5 {
		t.Fatalf("reason %+v", res)
	}
}

// BenchmarkStopQuestionNudge_NoQuestions measures the check on a reply that
// ends with a prose question when no item has a question record. The item
// count is the cost driver: the check reads each item's tag once.
func BenchmarkStopQuestionNudge_NoQuestions(b *testing.B) {
	for _, n := range []int{0, 20, 750} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			root := b.TempDir()
			d := filepath.Join(root, ".wipnote", "features")
			_ = os.MkdirAll(d, 0o755)
			body := `<article id="feat-x" data-type="feature"><header><h1>T</h1></header>` + strings.Repeat("<p>filler text</p>", 200) + `</article>`
			for i := 0; i < n; i++ {
				_ = os.WriteFile(filepath.Join(d, fmt.Sprintf("feat-%08d.html", i)), []byte(body), 0o644)
			}
			ev := &CloudEvent{LastAssistantMessage: "Shall I proceed?"}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = stopQuestionNudge(ev, root)
			}
		})
	}
}

// BenchmarkStopQuestionNudge_Gates measures the cheap checks that run before
// any store read (a reply that does not end with a question).
func BenchmarkStopQuestionNudge_Gates(b *testing.B) {
	ev := &CloudEvent{LastAssistantMessage: "All done, tests pass."}
	for i := 0; i < b.N; i++ {
		_ = stopQuestionNudge(ev, "")
	}
}
