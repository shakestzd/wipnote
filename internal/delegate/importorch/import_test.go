package importorch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// fixtureHostDir stands in for a developer's project folder. It is built at
// run time so no host path is checked in under testdata.
var fixtureHostDir = "/" + "Users" + "/fixture-owner"

// fixture copies testdata/<name> into a temp folder, filling the placeholders
// {{MAILROOT}} (the copied mail root) and {{HOSTDIR}}. It returns the registry
// path and the mail root.
func fixture(t *testing.T, name string) (string, string) {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	src := filepath.Join("testdata", name)
	dst := t.TempDir()
	mailRoot := filepath.Join(dst, "mail")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		s := strings.ReplaceAll(string(data), "{{MAILROOT}}", mailRoot)
		s = strings.ReplaceAll(s, "{{HOSTDIR}}", fixtureHostDir)
		return os.WriteFile(out, []byte(s), 0o600)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return filepath.Join(dst, "registry.json"), mailRoot
}

func newStore(t *testing.T) *delegationledger.Store {
	t.Helper()
	return delegationledger.NewStore(filepath.Join(t.TempDir(), ".wipnote"))
}

func importBasic(t *testing.T) (*delegationledger.Store, Result, string) {
	t.Helper()
	reg, root := fixture(t, "basic")
	s := newStore(t)
	res, err := Import(s, Options{RegistryPath: reg, MailRoots: []string{root}})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return s, res, root
}

func rows(t *testing.T, s *delegationledger.Store) []delegationledger.Record {
	t.Helper()
	recs, err := s.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return recs
}

// byName returns the rows for one orch session name, oldest first.
func byName(t *testing.T, s *delegationledger.Store, name string) []delegationledger.Record {
	t.Helper()
	var out []delegationledger.Record
	for _, r := range rows(t, s) {
		if r.Name == name || (r.Name == "" && strings.HasPrefix(name, "oneshot:") && r.Harness == name[8:]) {
			out = append(out, r)
		}
	}
	return out
}

func only(t *testing.T, s *delegationledger.Store, name string) delegationledger.Record {
	t.Helper()
	got := byName(t, s, name)
	if len(got) != 1 {
		t.Fatalf("rows for %s = %d, want 1", name, len(got))
	}
	return got[0]
}

func TestImportOrch_Idempotent(t *testing.T) {
	s, first, _ := importBasic(t)
	if len(first.Imported) != 4 {
		t.Fatalf("first import wrote %d rows (%v), want 4", len(first.Imported), first.Imported)
	}
	before, _ := os.ReadFile(s.Path())

	reg, root := fixture(t, "basic") // a fresh copy at another path: ids must not depend on paths
	second, err := Import(s, Options{RegistryPath: reg, MailRoots: []string{root}})
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if len(second.Imported) != 0 || second.Existing != 4 {
		t.Fatalf("rerun imported %v, existing %d; want none and 4", second.Imported, second.Existing)
	}
	after, _ := os.ReadFile(s.Path())
	if string(before) != string(after) {
		t.Fatal("a rerun changed the ledger")
	}
	idRe := regexp.MustCompile(`^dlg-[0-9a-f]{16}$`)
	for _, r := range rows(t, s) {
		if !idRe.MatchString(r.ID) {
			t.Errorf("id %q is not dlg-<16 hex>", r.ID)
		}
	}
}

func TestImportOrch_DropsHostPathsAndLabels(t *testing.T) {
	s, _, root := importBasic(t)
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	for _, bad := range []string{fixtureHostDir, root, "/workspaces/", "derived-alpha", "w1-p1"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("ledger carries %q", bad)
		}
	}
	for _, r := range rows(t, s) {
		if r.Label != "" {
			t.Errorf("row %s label %q, want empty (explicit labels only)", r.ID, r.Label)
		}
	}
	// The path-style caller session keeps only its session id.
	if got := byName(t, s, "alpha")[1].CallerSession; got != "0b9c6a1e-2f3d-4c5b-8a79-1e2d3c4b5a69" {
		t.Errorf("alpha caller session %q, want the bare session id", got)
	}
	if got := only(t, s, "oneshot:codex").CallerSession; got != "caller-plain-1" {
		t.Errorf("beta caller session %q, want caller-plain-1", got)
	}
}

func TestImportOrch_EndReasonImported(t *testing.T) {
	s, res, _ := importBasic(t)
	for _, r := range rows(t, s) {
		if r.EndReason != delegationledger.EndImported || r.IsOpen() {
			t.Errorf("row %s end_reason %q open=%v, want imported and closed", r.ID, r.EndReason, r.IsOpen())
		}
	}
	if res.Skipped["unsupported kind pi"] != 1 {
		t.Errorf("skipped = %v, want the pi session skipped by kind", res.Skipped)
	}
}

func TestImportOrch_LinksBySessionID(t *testing.T) {
	s, _, _ := importBasic(t)
	alpha := byName(t, s, "alpha")[1] // the live record; [0] is the archived run
	if alpha.WorkItem != "" || alpha.Link != "sid" {
		t.Fatalf("alpha work_item %q link %q, want none and sid", alpha.WorkItem, alpha.Link)
	}
	want := []string{"7d1f2e3a-4b5c-4d6e-9f80-a1b2c3d4e5f6", "sess-alpha-after-clear"}
	if strings.Join(alpha.WorkerSessions, " ") != strings.Join(want, " ") {
		t.Fatalf("alpha worker sessions %v, want %v", alpha.WorkerSessions, want)
	}
	if alpha.Harness != "claude" || alpha.Mode != delegationledger.ModeInteractive || alpha.Host != "host" {
		t.Errorf("alpha harness/mode/host = %s/%s/%s", alpha.Harness, alpha.Mode, alpha.Host)
	}
	if beta := only(t, s, "oneshot:codex"); beta.Link != "" || beta.WorkerSessions != nil {
		t.Errorf("beta has no events: link %q sessions %v, want none", beta.Link, beta.WorkerSessions)
	}
}

func TestImportOrch_KeepsWorkItemWhenPresent(t *testing.T) {
	s, _, _ := importBasic(t)
	gamma := only(t, s, "gamma")
	if gamma.WorkItem != "feat-0a1b2c3d" || gamma.Link != "tag" {
		t.Fatalf("gamma work_item %q link %q, want feat-0a1b2c3d and tag", gamma.WorkItem, gamma.Link)
	}
	if gamma.Host != "container" {
		t.Errorf("gamma host %q, want container", gamma.Host)
	}
	if only(t, s, "oneshot:codex").WorkItem != "" {
		t.Error("a record with no work_item field gained one")
	}
}

func TestImportOrch_EndedFromLastEvent(t *testing.T) {
	s, _, _ := importBasic(t)
	alpha := byName(t, s, "alpha")[1]
	if !alpha.Started.Equal(epoch(1790000000)) {
		t.Errorf("alpha started %v, want created_at", alpha.Started)
	}
	// The torn last line (no newline) is not an event.
	if !alpha.Ended.Equal(epoch(1790000500.5)) {
		t.Errorf("alpha ended %v, want the last complete event %v", alpha.Ended, epoch(1790000500.5))
	}
}

func TestImportOrch_NoEventsLeavesEndedEmpty(t *testing.T) {
	s, _, _ := importBasic(t)
	beta := only(t, s, "oneshot:codex")
	if !beta.Ended.IsZero() {
		t.Fatalf("beta ended %v, want empty", beta.Ended)
	}
	if beta.Mode != delegationledger.ModeOneshot || beta.Name != "" {
		t.Errorf("beta mode %q name %q, want oneshot and no name", beta.Mode, beta.Name)
	}
	if !beta.Started.Equal(epoch(1790000100)) {
		t.Errorf("beta started %v, want created_at", beta.Started)
	}
}

func TestImportOrch_ReadsArchivedMailboxes(t *testing.T) {
	reg, root := fixture(t, "basic")
	s := newStore(t)
	res, err := Import(s, Options{RegistryPath: reg, MailRoots: []string{root}})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	runs := byName(t, s, "alpha")
	if len(runs) != 2 {
		t.Fatalf("alpha runs = %d, want 2 (archived and live)", len(runs))
	}
	old := runs[0]
	if strings.Join(old.WorkerSessions, " ") != "sess-alpha-old" || old.Link != "sid" {
		t.Errorf("archived run sessions %v link %q", old.WorkerSessions, old.Link)
	}
	if !old.Started.Equal(epoch(1789990000)) || !old.Ended.Equal(epoch(1789990900)) {
		t.Errorf("archived run %v..%v, want its first and last events", old.Started, old.Ended)
	}
	if old.Harness != "claude" || old.Host != "container" {
		t.Errorf("archived run harness %q host %q, want claude and container (launcher.pid)", old.Harness, old.Host)
	}
	if res.Skipped["archived mailbox without hook events"] != 1 {
		t.Errorf("skipped = %v, want the event-less archive skipped", res.Skipped)
	}

	// Two-sided: without the archive folder the older run is not invented.
	if err := os.RemoveAll(filepath.Join(root, ".archive")); err != nil {
		t.Fatal(err)
	}
	fresh := newStore(t)
	if _, err := Import(fresh, Options{RegistryPath: reg, MailRoots: []string{root}}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if n := len(byName(t, fresh, "alpha")); n != 1 {
		t.Errorf("alpha runs without archive = %d, want 1", n)
	}
}

func TestImportOrch_MailRootFlag(t *testing.T) {
	reg, root := fixture(t, "basic")
	moved := filepath.Join(t.TempDir(), "moved-mail")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORCH_MAIL_ROOT", t.TempDir()) // an empty root the flag must outrank

	stale := newStore(t)
	if _, err := Import(stale, Options{RegistryPath: reg, MailRoots: MailRoots("", os.Getenv, t.TempDir())}); err != nil {
		t.Fatalf("Import without flag: %v", err)
	}
	if n := len(byName(t, stale, "alpha")); n != 1 || byName(t, stale, "alpha")[0].WorkerSessions != nil {
		t.Fatalf("without --mail-root the moved mailboxes must not be found (alpha rows %d)", n)
	}

	s := newStore(t)
	roots := MailRoots(moved, os.Getenv, t.TempDir())
	if len(roots) != 1 || roots[0] != moved {
		t.Fatalf("MailRoots with a flag = %v, want only the flag", roots)
	}
	if _, err := Import(s, Options{RegistryPath: reg, MailRoots: roots, Explicit: true}); err != nil {
		t.Fatalf("Import with flag: %v", err)
	}
	if runs := byName(t, s, "alpha"); len(runs) != 2 || runs[1].WorkerSessions == nil {
		t.Fatalf("with --mail-root, alpha rows %d; want the archived and live runs with sessions", len(runs))
	}
}

func TestImportOrch_MailRootDefaults(t *testing.T) {
	home := "/nonexistent-home"
	env := map[string]string{"ORCH_MAIL_ROOT": "/r/mail", "ORCH_SHARED_ROOT": "/r/shared"}
	got := MailRoots("", func(k string) string { return env[k] }, home)
	want := []string{"/r/mail", "/r/shared/.orch/mail", home + "/work/.orch/mail", home + "/work/adhoc/orch-mail"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("MailRoots = %v, want %v", got, want)
	}
}

func TestImportOrch_EventsV1(t *testing.T) {
	s, _, _ := importBasic(t)
	if got := byName(t, s, "alpha")[1].WorkerSessions; len(got) != 2 {
		t.Fatalf("v1 events: alpha sessions %v, want 2", got)
	}
}

func TestImportOrch_EventsV2(t *testing.T) {
	s, _, _ := importBasic(t)
	gamma := only(t, s, "gamma")
	if strings.Join(gamma.WorkerSessions, " ") != "sess-gamma-1" || !gamma.Ended.Equal(epoch(1790000400)) {
		t.Fatalf("v2 events: gamma sessions %v ended %v", gamma.WorkerSessions, gamma.Ended)
	}
}

func TestImportOrch_RefusesUnknownVersion(t *testing.T) {
	reg, root := fixture(t, "badversion")
	s := newStore(t)
	_, err := Import(s, Options{RegistryPath: reg, MailRoots: []string{root}})
	if err == nil {
		t.Fatal("an event in format v3 was accepted")
	}
	if !strings.Contains(err.Error(), "v3") || !strings.Contains(err.Error(), "omega") {
		t.Errorf("error %q must name the version and the mailbox", err)
	}
	if strings.Contains(err.Error(), root) {
		t.Errorf("error %q prints a host path", err)
	}
	if _, statErr := os.Stat(s.Path()); !os.IsNotExist(statErr) {
		t.Error("a refused import wrote the ledger")
	}
}
