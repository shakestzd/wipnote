package render

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// orchestraFixture is the wipnote-orchestra mod's test fixture (plan-28b4a7f6
// slice 19): the run folders and ledger rows below, written by the real run
// store, ledger and renderers, then serialized as a TypeScript module the
// mod's runs.test.ts imports (the mods test kit loads only code files).
const orchestraFixture = "../../../mods/wipnote-orchestra/tests/fixtures/runs.ts"

// fixtureT0 is the fixtures' clock: the first run's start.
var fixtureT0 = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

// fixtureRoot stands for the temp project in every path the files hold.
const fixtureRoot = "/project"

// TestOrchestraFixture fails when the committed fixture is not what the Go
// writers produce today, so the mod's tests never read a guessed format.
// WIPNOTE_UPDATE_FIXTURES=1 rewrites it.
func TestOrchestraFixture(t *testing.T) {
	project := t.TempDir()
	groups := writeFixtureRuns(t, project)
	got := serializeFixture(t, project, groups)
	if os.Getenv("WIPNOTE_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(orchestraFixture, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(orchestraFixture)
	if err != nil {
		t.Fatalf("read %s (WIPNOTE_UPDATE_FIXTURES=1 writes it): %v", orchestraFixture, err)
	}
	same(t, "orchestra fixture (WIPNOTE_UPDATE_FIXTURES=1 rewrites it)", got, string(want))
}

// fixtureRun is one run folder and its ledger row.
type fixtureRun struct {
	rec      delegationledger.Record
	label    string // the run folder's label file
	host     string
	start    int // seconds after fixtureT0
	out      Output
	ended    int  // seconds after fixtureT0; 0 while live
	exit     int  // the exit file, when ended and !noExit
	noExit   bool // ended in the ledger only (a stopped worker)
	noLedger bool
	events   bool
}

func fixtureRuns(t *testing.T) []fixtureRun {
	claude := Claude([]byte(golden(t, "testdata/claude/claude.stream.jsonl")))
	codex := Codex([]byte(golden(t, "testdata/codex/normal.raw")))
	live := Codex([]byte(strings.Join(strings.SplitN(golden(t, "testdata/codex/normal.raw"), "\n", 3)[:2], "\n") + "\n"))
	live.Last = ""
	ccx := Ccx(nil, []byte("ccx: failed in 3s\nthe tests still fail\nline 2\n"))
	summary := Claude([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"\n\n## Summary\nAll three fixed.\nDetails follow.","usage":{"input_tokens":3,"output_tokens":9}}` + "\n"))
	rec := func(id, mode, harness, name, label string) delegationledger.Record {
		return delegationledger.Record{ID: id, Mode: mode, Harness: harness, Name: name, Label: label, WorkItem: "feat-00000001", Host: "host"}
	}
	one, inter := delegationledger.ModeOneshot, delegationledger.ModeInteractive
	return []fixtureRun{
		{rec: rec("dlg-0a0000000001", one, "claude-print", "", ""), label: "Find the tsconfig base", host: runstore.HostNone, start: 0, out: claude, ended: 40},
		{rec: rec("dlg-0a0000000002", one, "codex", "", `band "review" & <check>`), label: "band review", host: runstore.HostHerdr, start: 60, out: live},
		{rec: rec("dlg-0a0000000003", inter, "codex", "w1", ""), label: "worker w1", host: runstore.HostHerdr, start: 90, out: codex, events: true},
		{rec: rec("dlg-0a0000000004", one, "ccx", "", ""), label: "Fix the tests", host: runstore.HostNone, start: 100, out: ccx, ended: 130, exit: 2},
		{rec: rec("dlg-0a0000000005", inter, "claude", "w2", ""), label: "worker w2", host: runstore.HostClaude, start: 150, out: summary, ended: 200, noExit: true},
		{label: "orphan run", host: runstore.HostNone, start: 220, out: live, noLedger: true, rec: delegationledger.Record{ID: "dlg-0a0000000006"}},
	}
}

// writeFixtureRuns writes every run through runstore, render and the ledger,
// then lists them as `wipnote delegate list --all` does (which keeps each
// open worker's observed state in its folder) and returns each run's group.
func writeFixtureRuns(t *testing.T, project string) map[string]session.Group {
	t.Helper()
	store, err := runstore.New(project)
	if err != nil {
		t.Fatal(err)
	}
	ledger := delegationledger.NewStore(filepath.Join(project, ".wipnote"))
	at := func(s int) time.Time { return fixtureT0.Add(time.Duration(s) * time.Second) }
	for _, r := range fixtureRuns(t) {
		writeFixtureFolder(t, store, r, at)
		if r.noLedger {
			continue
		}
		r.rec.Started = at(r.start)
		if _, err := ledger.Open(r.rec); err != nil {
			t.Fatal(err)
		}
		if r.ended == 0 {
			continue
		}
		c := delegationledger.CloseInfo{Ended: at(r.ended), Reason: delegationledger.EndExit, Exit: &r.exit}
		if r.noExit {
			c.Reason, c.Exit = delegationledger.EndStop, nil
		}
		if err := ledger.Close(r.rec.ID, c); err != nil {
			t.Fatal(err)
		}
	}
	// A row whose folder was swept, and a folder that is no run: both unseen.
	swept := delegationledger.Record{ID: "dlg-0a0000000007", Mode: delegationledger.ModeOneshot, Harness: "codex", Started: at(-9000)}
	if _, err := ledger.Open(swept); err != nil {
		t.Fatal(err)
	}
	noise := filepath.Join(store.Root(), "runs", "not-a-run")
	if err := os.MkdirAll(noise, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noise, runstore.FileLabel), []byte("noise"), 0o600); err != nil {
		t.Fatal(err)
	}
	return listGroups(t, store, ledger)
}

// listGroups is each ledger run's group as list reports it (no herdr: files only).
func listGroups(t *testing.T, store *runstore.Store, ledger *delegationledger.Store) map[string]session.Group {
	t.Helper()
	rows, err := session.List(t.Context(), session.Deps{Store: store, Ledger: ledger}, true)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]session.Group{}
	for _, r := range rows {
		groups[r.Run] = r.Group
	}
	return groups
}

func writeFixtureFolder(t *testing.T, store *runstore.Store, r fixtureRun, at func(int) time.Time) {
	t.Helper()
	rf, err := store.CreateRun(r.rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			t.Helper()
			t.Fatal(err)
		}
	}
	must(rf.WriteLabel(r.label))
	must(rf.WriteHost(r.host))
	must(rf.WriteStart(at(r.start)))
	must(rf.WriteFile(runstore.FileCleanLog, []byte(r.out.CleanLog)))
	must(rf.WriteFile(runstore.FileCleanMD, []byte(r.out.CleanMD)))
	if r.out.Usage != "" {
		must(rf.WriteUsage(r.out.Usage))
	}
	if r.out.Last != "" {
		must(rf.WriteFile(runstore.FileLastMD, []byte(r.out.Last)))
	}
	if r.events {
		w, err := rf.OpenEvents("caller")
		must(err)
		must(w.Append(runstore.Event{TS: at(r.start).Format(time.RFC3339), Kind: runstore.KindSessionStart, SID: "sid-" + r.rec.Name}))
		must(w.Append(runstore.Event{TS: at(r.start + 5).Format(time.RFC3339), Kind: runstore.KindQuestion, SID: "sid-" + r.rec.Name}))
		must(w.Close())
		must(session.WriteInfo(rf, session.Info{Run: r.rec.ID, Name: r.rec.Name, Harness: r.rec.Harness, WorkItem: r.rec.WorkItem,
			Place: session.PlaceHost, Created: at(r.start).Format(time.RFC3339)}))
	}
	if r.ended != 0 && !r.noExit {
		must(rf.Finish(r.exit, at(r.ended)))
		_, err := WritePage(rf)
		must(err)
	}
}

// serializeFixture renders every file under the project's .wipnote/ as a
// TypeScript module, the temp folder written as fixtureRoot.
func serializeFixture(t *testing.T, project string, groups map[string]session.Group) string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(filepath.Join(project, ".wipnote"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(project, path)
		files[filepath.ToSlash(rel)] = strings.ReplaceAll(string(b), project, fixtureRoot)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("// Code generated by TestOrchestraFixture (internal/delegate/render/modfixture_test.go). DO NOT EDIT.\n")
	b.WriteString("// Run folders and ledger rows written by the Go run store, ledger and renderers.\n\n")
	b.WriteString("/** The fixtures' clock: unix seconds of the first run's start. */\n")
	b.WriteString("export const T0 = " + jsonOf(t, fixtureT0.Unix()) + "\n\n")
	b.WriteString("/** Where the files sit: the project root they are written under. */\n")
	b.WriteString("export const ROOT = " + jsonOf(t, fixtureRoot) + "\n\n")
	b.WriteString("/** Every file under the project's .wipnote/, by path from the project root. */\n")
	b.WriteString("export const FILES: Record<string, string> = {\n")
	for _, k := range keys {
		b.WriteString("  " + jsonOf(t, k) + ": " + jsonOf(t, files[k]) + ",\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("/** Each ledger run's Overview group as `wipnote delegate list --all` reports it. */\n")
	b.WriteString("export const GROUPS: Record<string, string> = " + jsonOf(t, groups) + "\n\n")
	labels := map[session.Group]string{}
	for _, g := range session.Groups {
		labels[g] = g.Label()
	}
	b.WriteString("/** Each Overview group's display name, as Go labels it. */\n")
	b.WriteString("export const GROUP_LABELS: Record<string, string> = " + jsonOf(t, labels) + "\n")
	return b.String()
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
