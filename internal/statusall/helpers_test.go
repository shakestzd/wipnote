package statusall

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/gitwatch"
	"github.com/shakestzd/wipnote/internal/registry"
)

// testNow is the fixed clock every test uses.
var testNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func init() {
	// No test may reach the developer's real project or session.
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		_ = os.Unsetenv(k)
	}
}

// tempProject makes a temp project folder with a git repo whose one commit is
// dated commitAt, and an empty .wipnote/. It returns the project dir.
func tempProject(t *testing.T, subject string, commitAt time.Time) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, nil, "init", "-q", "-b", "work")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte(subject), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, nil, "add", "README")
	date := commitAt.Format(time.RFC3339)
	git(t, dir, []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date},
		"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false",
		"commit", "-q", "-m", subject)
	return dir
}

func git(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeItem writes one work-item HTML file.
func writeItem(t *testing.T, project, id, typ, status, title string) {
	t.Helper()
	sub := map[string]string{"feature": "features", "bug": "bugs", "spike": "spikes"}[typ]
	dir := filepath.Join(project, ".wipnote", sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	html := fmt.Sprintf(`<!DOCTYPE html><html><head><title>%[4]s</title></head><body>`+
		`<article id="%[1]s" data-type="%[2]s" data-status="%[3]s" data-priority="medium" `+
		`data-created="2026-10-27T10:00:00" data-updated="2026-10-27T11:00:00">`+
		`<header><h1>%[4]s</h1></header></article></body></html>`, id, typ, status, title)
	if err := os.WriteFile(filepath.Join(dir, id+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writePlan writes a plan YAML file.
func writePlan(t *testing.T, project, id, body string) {
	t.Helper()
	dir := filepath.Join(project, ".wipnote", "plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sessA = "11111111-1111-4111-8111-111111111111"
const sessB = "22222222-2222-4222-8222-222222222222"

// openSession records a session start (and an end when ended is non-zero).
func openSession(t *testing.T, project, id, harness string, started, ended time.Time) {
	t.Helper()
	st := sessionledger.NewStore(filepath.Join(project, ".wipnote"))
	if _, err := st.Open(sessionledger.Record{SessionID: id, Harness: harness, StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	if !ended.IsZero() {
		if err := st.Close(id, ended); err != nil {
			t.Fatal(err)
		}
	}
}

// claim opens a claim episode for the root agent of session on item.
func claim(t *testing.T, project, session, item string, at time.Time) {
	t.Helper()
	st := claimledger.NewStore(filepath.Join(project, ".wipnote"))
	_, _, err := st.Open(session, claimledger.Episode{WorkItemID: item, SessionID: session,
		AgentID: "__root__", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
}

// run appends a run start and, when status is non-empty, its end.
func run(t *testing.T, project, session, key, desc, status string, exit int, started, ended time.Time) {
	t.Helper()
	st := runledger.NewStore(filepath.Join(project, ".wipnote"))
	id := runledger.RunIDFor(session, key)
	start := runledger.Event{RunID: id, Phase: runledger.PhaseStart, At: started, Kind: runledger.KindShell,
		Description: desc, Command: desc, Session: session, Harness: "claude"}
	if err := st.Append(start); err != nil {
		t.Fatal(err)
	}
	if status == "" {
		return
	}
	end := runledger.Event{RunID: id, Phase: runledger.PhaseEnd, At: ended, Kind: runledger.KindShell,
		Status: status, Exit: &exit, Session: session, FirstError: firstErr(status)}
	if err := st.Append(end); err != nil {
		t.Fatal(err)
	}
}

func firstErr(status string) string {
	if status == runledger.StatusFailed {
		return "FAIL TestSomething"
	}
	return ""
}

func entry(dir, name string, lastSeen time.Time) registry.Entry {
	return registry.Entry{ID: registry.ComputeID(dir), ProjectDir: dir, Name: name,
		LastSeen: lastSeen.UTC().Format(time.RFC3339)}
}

// fakeGH answers gh argv by prefix, in order of the rules. Unmatched argv fail.
type fakeGH struct {
	mu    sync.Mutex
	rules [][2]string // prefix, stdout
	err   error       // when set every call returns it (gh missing)
	block bool        // when set every call waits for ctx to end
	calls []string
}

func (f *fakeGH) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.calls = append(f.calls, key)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return nil, nil, -1, fmt.Errorf("gh: %w", ctx.Err())
	}
	if f.err != nil {
		return nil, nil, -1, f.err
	}
	for _, r := range f.rules {
		if strings.HasPrefix(key, r[0]) {
			return []byte(r[1]), nil, 0, nil
		}
	}
	return nil, []byte("unexpected"), 1, nil
}

// ghWithPR answers the reads for PR 42 on branch "work": two checks, one
// failing, approved and mergeable.
func ghWithPR() *fakeGH {
	return &fakeGH{rules: [][2]string{
		{"gh pr list --head work", `[{"number":42}]`},
		{"gh pr view 42 --json number,title", `{"number":42,"title":"Add the thing","headRefName":"work",` +
			`"baseRefName":"main","state":"OPEN","url":"https://example.invalid/o/r/pull/42"}`},
		{"gh pr checks 42", `[{"name":"test","state":"SUCCESS","bucket":"pass"},` +
			`{"name":"lint","state":"FAILURE","bucket":"fail"}]`},
		{"gh pr view 42 --json reviewDecision", `{"reviewDecision":"APPROVED","reviewRequests":[],` +
			`"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","baseRefOid":"abc"}`},
		{"gh api graphql", `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]},` +
			`"commits":{"nodes":[]}}}}}`},
	}}
}

func ghFactory(f *fakeGH) func(string) gitwatch.Runner {
	return func(string) gitwatch.Runner { return f }
}

// fakeHerdr is an in-process herdr agent source.
type fakeHerdr struct {
	agents []herdr.Agent
	err    error
	screen string
}

func (f *fakeHerdr) AgentList(context.Context) ([]herdr.Agent, error) { return f.agents, f.err }

func (f *fakeHerdr) Read(context.Context, herdr.Pin, int) (string, error) {
	if f.screen == "" {
		return "", herdr.ErrBadReply
	}
	return f.screen, nil
}

// baseOptions are the options every test starts from.
func baseOptions(entries ...registry.Entry) Options {
	return Options{Entries: entries, Now: func() time.Time { return testNow }, Loc: time.UTC,
		GHReason: SourceSkipped, HerdrReason: SourceNotAvailable + ": not inside a herdr pane"}
}

func findProject(t *testing.T, r Report, name string) Project {
	t.Helper()
	for _, p := range r.Projects {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("project %q not in report: %+v", name, r.Projects)
	return Project{}
}
