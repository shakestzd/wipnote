package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

// nudgeSession claims the nudge project's items; otherSession is a second
// session working in the same project.
const (
	nudgeSession = "019ee380-abcd-7000-8000-0000000000a1"
	otherSession = "019ee380-abcd-7000-8000-0000000000b2"
)

// claimItem opens a claim episode for session on item in the canonical
// claim ledger, as `feature start` does.
func claimItem(t testing.TB, root, session, item string) {
	t.Helper()
	store := claimledger.NewStore(filepath.Join(root, ".wipnote"))
	if _, _, err := store.Open(session, claimledger.Episode{WorkItemID: item, SessionID: session,
		RootSessionID: session, AgentID: db.AgentRootSentinel, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

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
	claimItem(t, root, nudgeSession, "feat-aaaa0001")
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
			res := stopQuestionNudge(&ev, root, nudgeSession)
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
	if res := stopQuestionNudge(&CloudEvent{LastAssistantMessage: "ok?"}, root, nudgeSession); res != nil {
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
		if res := stopQuestionNudge(&CloudEvent{LastAssistantMessage: "ok?"}, root, nudgeSession); res != nil {
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
		claimItem(t, root, nudgeSession, id)
	}
	res := stopQuestionNudge(&CloudEvent{LastAssistantMessage: "ok?"}, root, nudgeSession)
	if res == nil || !strings.Contains(res.Reason, "and 2 more") || strings.Count(res.Reason, "q-0") != 5 {
		t.Fatalf("reason %+v", res)
	}
}

// Two sessions in one project: each is nudged only for open questions on
// the items it claimed. A session with no claims is never blocked.
func TestStopQuestionNudge_OnlyTheStoppingSessionsClaims(t *testing.T) {
	t.Setenv(DelegateRunEnv, "")
	root := nudgeProject(t, true) // nudgeSession claims feat-aaaa0001, which has q-77 open
	other := strings.NewReplacer("feat-aaaa0001", "feat-bbbb0002", "&#34;open&#34;", "&#34;confirmed&#34;").Replace(nudgeItem)
	if err := os.WriteFile(filepath.Join(root, ".wipnote", "features", "feat-bbbb0002.html"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	claimItem(t, root, otherSession, "feat-bbbb0002")
	ev := &CloudEvent{LastAssistantMessage: "Shall I go on?"}
	if res := stopQuestionNudge(ev, root, otherSession); res != nil {
		t.Fatalf("session B was blocked by session A's question: %+v", res)
	}
	if res := stopQuestionNudge(ev, root, nudgeSession); res == nil || !strings.Contains(res.Reason, "q-77") {
		t.Fatalf("session A was not nudged for its own question: %+v", res)
	}
	if res := stopQuestionNudge(ev, root, "019ee380-abcd-7000-8000-0000000000c3"); res != nil {
		t.Fatalf("a session with no claims was blocked: %+v", res)
	}
	if res := stopQuestionNudge(ev, root, ""); res != nil {
		t.Fatalf("an unknown session was blocked: %+v", res)
	}
}

// A scan that runs out of its time budget allows the stop.
func TestStopQuestionNudge_DeadlineAllows(t *testing.T) {
	t.Setenv(DelegateRunEnv, "")
	root := nudgeProject(t, true)
	prev := nudgeBudget
	nudgeBudget = -time.Second
	t.Cleanup(func() { nudgeBudget = prev })
	if res := stopQuestionNudge(&CloudEvent{LastAssistantMessage: "ok?"}, root, nudgeSession); res != nil {
		t.Fatalf("blocked after the budget ran out: %+v", res)
	}
}

// BenchmarkStopQuestionNudge measures the check on a reply that ends with a
// prose question. items is the project's size; the session claims three of
// them, one with an open question, so it blocks every time.
func BenchmarkStopQuestionNudge(b *testing.B) {
	for _, n := range []int{20, 1200} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			root := b.TempDir()
			d := filepath.Join(root, ".wipnote", "features")
			_ = os.MkdirAll(d, 0o755)
			filler := strings.Repeat("<p>filler text</p>", 200)
			for i := 0; i < n; i++ {
				id := fmt.Sprintf("feat-%08d", i)
				body := strings.NewReplacer("feat-aaaa0001", id, "</header>", "</header>"+filler).Replace(nudgeItem)
				_ = os.WriteFile(filepath.Join(d, id+".html"), []byte(body), 0o644)
			}
			for i := 0; i < 3; i++ {
				claimItem(b, root, nudgeSession, fmt.Sprintf("feat-%08d", i*(n/3)))
			}
			ev := &CloudEvent{LastAssistantMessage: "Shall I proceed?"}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if stopQuestionNudge(ev, root, nudgeSession) == nil {
					b.Fatal("expected a block")
				}
			}
		})
	}
}

// BenchmarkStopQuestionNudge_Gates measures the cheap checks that run before
// any store read (a reply that does not end with a question).
func BenchmarkStopQuestionNudge_Gates(b *testing.B) {
	ev := &CloudEvent{LastAssistantMessage: "All done, tests pass."}
	for i := 0; i < b.N; i++ {
		_ = stopQuestionNudge(ev, "", "")
	}
}
