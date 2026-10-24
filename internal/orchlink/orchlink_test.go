package orchlink

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRegistry writes an orch registry in orch's own shape
// ({"v":1,"sessions":{name:record}}) and returns its path.
func writeRegistry(t *testing.T, dir string, recs map[string]map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"v": 1, "sessions": recs})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeEvents creates <dir>/<name>/events.ndjson with the given raw content
// and returns the mailbox directory.
func writeEvents(t *testing.T, dir, name, content string) string {
	t.Helper()
	mb := filepath.Join(dir, "mail", name)
	if err := os.MkdirAll(mb, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mb, "events.ndjson"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return mb
}

func ev(kind, sid, agent string) string {
	b, _ := json.Marshal(map[string]any{"v": 1, "seq": 1, "ts": 1.0, "session": "x", "kind": kind, "sid": sid, "agent_id": agent})
	return string(b) + "\n"
}

func names(ss []Session) map[string]string {
	out := map[string]string{}
	for _, s := range ss {
		out[s.Name] = s.Link
	}
	return out
}

func TestRegistryPathFollowsOrchsRule(t *testing.T) {
	env := map[string]string{EnvRegistry: "/x/r.json"}
	if got := RegistryPath(func(k string) string { return env[k] }, "/home/u"); got != "/x/r.json" {
		t.Errorf("ORCH_REGISTRY must win, got %q", got)
	}
	got := RegistryPath(func(string) string { return "" }, "/home/u")
	if got != filepath.Join("/home/u", ".cache", "orch", "registry.json") {
		t.Errorf("default must be orch's, got %q", got)
	}
}

func TestMissingRegistryIsNotAnErrorButUnreadableIs(t *testing.T) {
	dir := t.TempDir()
	got, err := ForWorkItem(filepath.Join(dir, "absent.json"), "feat-00000001", nil)
	if err != nil || got != nil {
		t.Fatalf("missing registry: want (nil, nil), got (%v, %v)", got, err)
	}
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ForWorkItem(corrupt, "feat-00000001", nil); err == nil {
		t.Error("corrupt registry must be an error, not an empty answer")
	}
	if _, err := ForWorkItem(dir, "feat-00000001", nil); err == nil {
		t.Error("a registry path that cannot be read must be an error, not an empty answer")
	}
}

// One registry, every link case: the tag decides when present; the session
// id join applies only to untagged sessions.
func TestForWorkItemSelectsByTagThenBySessionID(t *testing.T) {
	dir := t.TempDir()
	const wi, other, mine, stranger = "feat-aaaaaaaa", "feat-bbbbbbbb", "sid-mine", "sid-stranger"
	path := writeRegistry(t, dir, map[string]map[string]any{
		"tagged":       {"kind": "claude", "state": "idle", "created_at": 1, "work_item": wi, "mailbox": writeEvents(t, dir, "tagged", ev("prompt", stranger, ""))},
		"tagged-other": {"kind": "claude", "state": "idle", "created_at": 2, "work_item": other, "mailbox": writeEvents(t, dir, "tagged-other", ev("prompt", mine, ""))},
		"untagged-hit": {"kind": "claude", "state": "working", "created_at": 3, "mailbox": writeEvents(t, dir, "untagged-hit", ev("prompt", mine, ""))},
		"untagged-off": {"kind": "codex", "state": "idle", "created_at": 4, "mailbox": writeEvents(t, dir, "untagged-off", ev("prompt", stranger, ""))},
		"no-mailbox":   {"kind": "claude", "state": "spawning", "created_at": 5},
	})
	got, err := ForWorkItem(path, wi, map[string]bool{mine: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"tagged": LinkTag, "untagged-hit": LinkSID}
	if g := names(got); len(g) != len(want) || g["tagged"] != want["tagged"] || g["untagged-hit"] != want["untagged-hit"] {
		t.Fatalf("want %v, got %v", want, g)
	}
	// Two-sided: the other work item sees its own tagged session and nothing derived from ours.
	got, _ = ForWorkItem(path, other, map[string]bool{})
	if g := names(got); len(g) != 1 || g["tagged-other"] != LinkTag {
		t.Errorf("other work item: want only tagged-other, got %v", g)
	}
	// Without any session ids, only tags link.
	got, _ = ForWorkItem(path, wi, nil)
	if g := names(got); len(g) != 1 || g["tagged"] != LinkTag {
		t.Errorf("no session ids: want only the tagged session, got %v", g)
	}
}

func TestEventsGiveDistinctSessionIDsAndSubagentsAndSkipPartialLines(t *testing.T) {
	dir := t.TempDir()
	content := ev("session_start", "s1", "") + "garbage\n" + ev("subagent_start", "s1", "a1") +
		ev("subagent_start", "s1", "a1") + ev("subagent_start", "s2", "a2") + ev("subagent_stop", "s2", "a3") +
		ev("prompt", "s2", "") + strings.TrimSuffix(ev("prompt", "s-partial", ""), "\n")
	mb := writeEvents(t, dir, "w1", content)
	sids, subagents := readEvents(mb)
	if strings.Join(sids, ",") != "s1,s2" {
		t.Errorf("want distinct ids in first-seen order without the partial line, got %v", sids)
	}
	if subagents != 2 {
		t.Errorf("want 2 distinct sub-agents started, got %d", subagents)
	}
	if s, n := readEvents(filepath.Join(dir, "mail", "absent")); s != nil || n != 0 {
		t.Errorf("missing log must give nothing, got %v %d", s, n)
	}
}

func TestSymlinkedEventLogIsNotFollowed(t *testing.T) {
	dir := t.TempDir()
	real := writeEvents(t, dir, "real", ev("prompt", "s-secret", ""))
	mb := filepath.Join(dir, "mail", "link")
	if err := os.MkdirAll(mb, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "events.ndjson"), filepath.Join(mb, "events.ndjson")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if sids, _ := readEvents(mb); len(sids) != 0 {
		t.Errorf("a symlinked log must be ignored, got %v", sids)
	}
	if sids, _ := readEvents(real); len(sids) != 1 {
		t.Errorf("control: the real log must be read, got %v", sids)
	}
}

func TestTailWindowDropsTheCutFirstLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("aaaa\nbb\ncc\ndd"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 14 bytes; the last 8 start inside "bb": the cut "b" and the unterminated "dd" are dropped.
	if got := string(readTail(path, 8)); got != "cc\n" {
		t.Errorf("want only complete lines, got %q", got)
	}
	for _, max := range []int64{1, 3, 5, 9, 100} {
		got := string(readTail(path, max))
		if got != "" && !strings.HasSuffix(got, "\n") {
			t.Errorf("max %d: last line must be complete, got %q", max, got)
		}
		for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
			if line != "" && line != "aaaa" && line != "bb" && line != "cc" {
				t.Errorf("max %d: line %q is a fragment", max, line)
			}
		}
	}
}

func TestSessionJSONCarriesNoHostPath(t *testing.T) {
	dir := t.TempDir()
	mb := writeEvents(t, dir, "w1", ev("prompt", "s1", ""))
	path := writeRegistry(t, dir, map[string]map[string]any{
		"w1": {"kind": "claude", "state": "idle", "created_at": 1, "work_item": "feat-aaaaaaaa", "mailbox": mb, "cwd": dir},
	})
	got, err := ForWorkItem(path, "feat-aaaaaaaa", nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("want one session, got %v %v", got, err)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), dir) {
		t.Errorf("output must not carry host paths: %s", b)
	}
}

// Claude can send a path-style session id; wipnote's hooks keep only its UUID.
// The reader must do the same, or the session id join misses and the path leaks.
func TestPathStyleSessionIDsAreNormalisedLikeWipnotesHooks(t *testing.T) {
	dir := t.TempDir()
	const uuid = "550e8400-e29b-41d4-a716-446655440000"
	pathID := filepath.Join(dir, "claude-501", "project", uuid)
	path := writeRegistry(t, dir, map[string]map[string]any{
		"w1": {"kind": "claude", "state": "idle", "created_at": 1,
			"mailbox": writeEvents(t, dir, "w1", ev("prompt", pathID, "")+ev("prompt", uuid, ""))},
	})
	got, err := ForWorkItem(path, "feat-aaaaaaaa", map[string]bool{uuid: true})
	if err != nil || len(got) != 1 || got[0].Link != LinkSID {
		t.Fatalf("a path-style id must join its UUID session, got %v %v", got, err)
	}
	if ids := got[0].SessionIDs; len(ids) != 1 || ids[0] != uuid {
		t.Errorf("want the one normalised id %q, got %v", uuid, ids)
	}
	if b, _ := json.Marshal(got); strings.Contains(string(b), dir) {
		t.Errorf("a path-style id must not leak a host path: %s", b)
	}
}
