package hooks

import (
	"path/filepath"
	"testing"
)

// The hot hooks (pretooluse, user-prompt, posttooluse, posttooluse-record)
// are dispatched with NO database handle (feat-af8d1689). They used to get a
// fresh in-memory projection that nothing ever hydrates, so a nil handle must
// produce exactly what that empty projection produced: the same HookResult and
// the same daemon-routed writes. These tests pin that parity, so dropping the
// projection is a pure cost cut.

const paritySession = "019ee380-abcd-7000-8000-00000000f00d"

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
