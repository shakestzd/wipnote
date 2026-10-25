package hooks

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

// The hot hooks (pretooluse, user-prompt, posttooluse, posttooluse-record)
// are dispatched with NO database handle (feat-af8d1689). They used to get a
// fresh in-memory projection that nothing ever hydrates, so a nil handle must
// produce exactly what that empty projection produced: the same HookResult and
// the same daemon-routed writes. These tests pin that parity, so dropping the
// projection is a pure cost cut.

const paritySession = "019ee380-abcd-7000-8000-00000000f00d"

// parityCase is one hook call, plus optional canonical state to seed first.
type parityCase struct {
	name    string
	handler func(*CloudEvent, *sql.DB) (*HookResult, error)
	event   func(projectDir string) *CloudEvent
	seed    func(projectDir string)
}

// parityOutcome is what a hook call is observable by: its result and the
// statements it routed to the writer daemon.
type parityOutcome struct {
	Result string
	Routed []string
}

func parityCases() []parityCase {
	tool := func(name string, input map[string]any) func(string) *CloudEvent {
		return func(projectDir string) *CloudEvent {
			return &CloudEvent{SessionID: paritySession, CWD: projectDir, ToolName: name,
				ToolUseID: "toolu_parity", ToolInput: input, Harness: HarnessClaude,
				ToolResult: map[string]any{"stdout": "ok"}}
		}
	}
	editIn := func(projectDir string) map[string]any {
		return map[string]any{"file_path": filepath.Join(projectDir, "main.go"), "old_string": "a", "new_string": "b"}
	}
	claimedOnlyWrites := func(projectDir string) {
		store := claimledger.NewStore(filepath.Join(projectDir, ".wipnote"))
		_, _, _ = store.Open(paritySession, claimledger.Episode{WorkItemID: "feat-parity",
			SessionID: paritySession, RootSessionID: paritySession,
			AgentID: db.AgentRootSentinel, StartedAt: time.Now().UTC()})
		appendCanonicalToolEvent(projectDir, paritySession, canonicalToolEvent{Tool: "Write", Summary: "x.go"})
	}
	return []parityCase{
		{name: "pretooluse WebSearch (orchestrator advisory)", handler: PreToolUse,
			event: tool("WebSearch", map[string]any{"query": "sqlite"})},
		{name: "pretooluse Bash", handler: PreToolUse,
			event: tool("Bash", map[string]any{"command": "ls -la"})},
		{name: "pretooluse Edit without work item", handler: PreToolUse,
			event: func(p string) *CloudEvent { return tool("Edit", editIn(p))(p) }},
		{name: "pretooluse Edit, claimed, no research in canonical log", handler: PreToolUse, seed: claimedOnlyWrites,
			event: func(p string) *CloudEvent { return tool("Edit", editIn(p))(p) }},
		{name: "posttooluse Bash", handler: PostToolUseForHarness,
			event: tool("Bash", map[string]any{"command": "ls -la"})},
		{name: "posttooluse-record Bash", handler: PostToolUseRecord,
			event: tool("Bash", map[string]any{"command": "ls -la"})},
		{name: "user-prompt", handler: UserPrompt, event: func(p string) *CloudEvent {
			return &CloudEvent{SessionID: paritySession, CWD: p, Prompt: "list the files", Harness: HarnessClaude}
		}},
	}
}

// runParity runs c once in a fresh project against database (nil, or an empty
// projection when withProjection is set) and returns what it did.
func runParity(t *testing.T, c parityCase, withProjection bool) parityOutcome {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectDir)
	featureIDCache = featureIDCacheEntry{}
	if c.seed != nil {
		c.seed(projectDir)
	}
	var out parityOutcome
	prev := routeSQLAsync
	routeSQLAsync = func(_, sqlStmt string, _ ...any) bool {
		out.Routed = append(out.Routed, strings.Join(strings.Fields(sqlStmt), " "))
		return true
	}
	defer func() { routeSQLAsync = prev }()

	var database *sql.DB
	if withProjection {
		var err error
		if database, err = db.OpenEphemeralProjection(); err != nil {
			t.Fatal(err)
		}
		defer database.Close()
	}
	res, err := c.handler(c.event(projectDir), database)
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	raw, _ := json.Marshal(res)
	out.Result = strings.ReplaceAll(string(raw), projectDir, "<project>")
	return out
}

func isolateParityEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"XDG_DATA_HOME":       filepath.Join(home, ".local", "share"),
		"WIPNOTE_PROJECT_DIR": "", "CLAUDE_PROJECT_DIR": "", "CLAUDE_ENV_FILE": "",
		"WIPNOTE_YOLO": "", "WIPNOTE_GUARDS_OFF": "", "WIPNOTE_AGENT_ID": "",
		"WIPNOTE_PARENT_EVENT": "", "WIPNOTE_PARENT_PROMPT_EVENT": "",
	} {
		t.Setenv(k, v)
	}
}

// TestReadOnlyHooks_NilMatchesEmptyProjection: every hot-hook case gives the
// same result and routes the same writes with no handle as with the empty
// projection.
func TestReadOnlyHooks_NilMatchesEmptyProjection(t *testing.T) {
	isolateParityEnv(t)
	for _, c := range parityCases() {
		t.Run(c.name, func(t *testing.T) {
			want := runParity(t, c, true)
			got := runParity(t, c, false)
			if got.Result != want.Result {
				t.Errorf("result differs\n nil:        %s\n projection: %s", got.Result, want.Result)
			}
			if strings.Join(got.Routed, "\n") != strings.Join(want.Routed, "\n") {
				t.Errorf("routed writes differ\n nil:        %q\n projection: %q", got.Routed, want.Routed)
			}
		})
	}
}

// TestResearchGuards_NilReadsCanonicalLog pins the two research helpers on a
// nil handle: they answer from the canonical tool log and fail open only when
// no log exists.
func TestResearchGuards_NilReadsCanonicalLog(t *testing.T) {
	isolateParityEnv(t)
	projectDir := t.TempDir()
	if hasRecentResearch(nil, paritySession, "", projectDir) != true {
		t.Fatal("no canonical log: want fail-open true")
	}
	appendCanonicalToolEvent(projectDir, paritySession, canonicalToolEvent{Tool: "Write", Summary: "x.go"})
	if hasRecentResearch(nil, paritySession, "", projectDir) {
		t.Fatal("canonical log without research: want false")
	}
	appendCanonicalToolEvent(projectDir, paritySession, canonicalToolEvent{Tool: "Read", Summary: "x.go"})
	if !hasRecentResearch(nil, paritySession, "", projectDir) {
		t.Fatal("canonical log with a Read: want true")
	}
	// Web research reads the same log (canonical_guards_test.go covers it):
	// a Read and a Write are not web research.
	if hasRecentWebResearch(nil, paritySession, "", projectDir) {
		t.Fatal("canonical log without web research: want false")
	}
}
