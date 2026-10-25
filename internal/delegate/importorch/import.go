// Package importorch is `wipnote delegate import-orch`: a one-shot,
// idempotent copy of orch's run history into the delegations ledger
// (plan-28b4a7f6 slice 18, feat-3d08f822), so lineage keeps orch runs after
// internal/orchlink is retired (slice 13).
//
// One ledger row per past orch run: each registry record, plus each archived
// mailbox (a name reused at spawn replaced the old record, so older runs exist
// only as <mail_root>/.archive/<name>-<epoch>-<token>/). Every row is closed
// with end_reason=imported; ended is the last event's time, or empty. Rows
// link by worker session id; a work_item field, which orch 0.1.0 never
// writes, gives link=tag only when present. Host paths and labels never enter
// a row. The dlg- id is a hash of orch's identity for the run, never of a
// path, so a rerun finds every row and adds nothing.
//
// This reader is self-contained on purpose: slice 13 deletes
// internal/orchlink, and the import must keep building after that.
package importorch

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// Options says where orch's files are.
type Options struct {
	RegistryPath string   // orch's registry.json
	MailRoots    []string // mail roots holding .archive/ (see MailRoots)
	// Explicit is set when MailRoots came from --mail-root: live mailboxes are
	// then looked up under it instead of at the record's stored path, which
	// may name a root that has since moved.
	Explicit bool
}

// Result reports one import. Skipped counts runs not imported, by reason.
type Result struct {
	Imported []string
	Existing int
	Skipped  map[string]int
}

// candidate is one row to write and, for an archived run, its name and first
// worker session, which also identify a run imported earlier from the registry.
type candidate struct {
	rec      delegationledger.Record
	archived bool
}

// Import reads every orch run first and refuses before writing anything if
// any input is unreadable or in an unknown format, then appends the rows the
// ledger does not hold yet.
func Import(store *delegationledger.Store, opts Options) (Result, error) {
	res := Result{Skipped: map[string]int{}}
	cands, err := collect(opts, res.Skipped)
	if err != nil {
		return res, err
	}
	existing, err := store.ReadAll()
	if err != nil {
		return res, err
	}
	have, runKeys := map[string]bool{}, map[string]bool{}
	for _, r := range existing {
		have[r.ID] = true
		runKeys[runKey(r)] = true
	}
	for _, c := range cands {
		if have[c.rec.ID] || (c.archived && runKeys[runKey(c.rec)]) {
			res.Existing++
			continue
		}
		if _, err := store.Open(c.rec); err != nil {
			return res, err
		}
		have[c.rec.ID] = true
		res.Imported = append(res.Imported, c.rec.ID)
	}
	return res, nil
}

// runKey identifies a run by name and first worker session. A run imported
// while live and found archived on a later rerun has the same key.
func runKey(r delegationledger.Record) string {
	if len(r.WorkerSessions) == 0 {
		return ""
	}
	return r.Name + "\x00" + r.WorkerSessions[0]
}

// collect turns the registry records and the archived mailboxes into rows.
func collect(opts Options, skipped map[string]int) ([]candidate, error) {
	recs, err := loadRegistry(opts.RegistryPath)
	if err != nil {
		return nil, err
	}
	roots := append([]string(nil), opts.MailRoots...)
	var out []candidate
	for _, o := range recs {
		if !opts.Explicit && o.Mailbox != "" {
			roots = append(roots, filepath.Dir(o.Mailbox))
		}
		row, reason, err := fromRecord(o, opts)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			skipped[reason]++
			continue
		}
		out = append(out, candidate{rec: row})
	}
	arch, err := collectArchives(dedupe(roots), skipped)
	return append(out, arch...), err
}

func collectArchives(roots []string, skipped map[string]int) ([]candidate, error) {
	var out []candidate
	seen := map[string]bool{}
	for _, root := range roots {
		for _, a := range listArchives(root) {
			if seen[a.folder] {
				continue
			}
			seen[a.folder] = true
			row, reason, err := fromArchive(a)
			if err != nil {
				return nil, err
			}
			if reason != "" {
				skipped[reason]++
				continue
			}
			out = append(out, candidate{rec: row, archived: true})
		}
	}
	return out, nil
}

// kinds maps orch's kind to the ledger harness and the mode when the record
// has none. pi has no ledger harness and is skipped.
var kinds = map[string]struct{ harness, mode string }{
	"claude":     {"claude", delegationledger.ModeInteractive},
	"codex":      {"codex", delegationledger.ModeInteractive},
	"claude_p":   {"claude-print", delegationledger.ModeOneshot},
	"codex_exec": {"codex", delegationledger.ModeOneshot},
	"ccx_task":   {"ccx", delegationledger.ModeOneshot},
}

// fromRecord builds the row for one registry record, or a skip reason.
func fromRecord(o orchRecord, opts Options) (delegationledger.Record, string, error) {
	k, ok := kinds[o.Kind]
	if !ok {
		return delegationledger.Record{}, "unsupported kind " + o.Kind, nil
	}
	ev, err := readEvents(liveMailbox(o, opts), o.Name)
	if err != nil {
		return delegationledger.Record{}, "", err
	}
	started := ev.first
	if o.CreatedAt > 0 {
		started = epoch(o.CreatedAt)
	}
	if started.IsZero() {
		return delegationledger.Record{}, "no start time", nil
	}
	host := "host"
	if o.Container || o.Kind == "ccx_task" {
		host = "container"
	}
	r := baseRow(recordID(o), modeOf(o, k.mode), k.harness, o.Name, host, started, ev)
	if o.LaunchedBy != nil {
		r.CallerSession = cleanSessionID(o.LaunchedBy.Session)
	}
	if workItemPattern.MatchString(o.WorkItem) {
		r.WorkItem, r.Link = o.WorkItem, "tag"
	}
	return r, "", r.Validate()
}

// fromArchive builds the row for an archived mailbox. orch installs hooks only
// for interactive Claude workers (adapters/claude.py, container_claude.py), so
// a mailbox with hook events is one; one without events carries nothing that
// links it or names its harness, and is skipped.
func fromArchive(a archived) (delegationledger.Record, string, error) {
	ev, err := readEvents(a.dir, a.name)
	if err != nil {
		return delegationledger.Record{}, "", err
	}
	if ev.count == 0 || ev.first.IsZero() {
		return delegationledger.Record{}, "archived mailbox without hook events", nil
	}
	host := "" // unknown: a container worker may run without the launcher
	if isFile(filepath.Join(a.dir, "launcher.pid")) {
		host = "container"
	}
	r := baseRow(hashID("orch-archive", a.folder), delegationledger.ModeInteractive, "claude", a.name, host, ev.first, ev)
	return r, "", r.Validate()
}

func baseRow(id, mode, harness, name, host string, started time.Time, ev events) delegationledger.Record {
	r := delegationledger.Record{
		ID: id, Mode: mode, Harness: harness, Host: host, Started: started,
		WorkerSessions: ev.sids, EndReason: delegationledger.EndImported,
	}
	if mode == delegationledger.ModeInteractive {
		r.Name = name
	}
	if !ev.last.IsZero() {
		r.Ended = ev.last
		if r.Ended.Before(started) {
			r.Ended = started
		}
	}
	if len(ev.sids) > 0 {
		r.Link = "sid"
	}
	return r
}

// workItemPattern matches the ledger's work-item column.
var workItemPattern = regexp.MustCompile(`^(feat|bug|spk)-[0-9a-z]+$`)

func modeOf(o orchRecord, fallback string) string {
	switch o.Mode {
	case "headless":
		return delegationledger.ModeOneshot
	case "interactive":
		return delegationledger.ModeInteractive
	}
	return fallback
}

// liveMailbox is the record's mailbox folder: under --mail-root when given,
// else at the path the record stores.
func liveMailbox(o orchRecord, opts Options) string {
	if opts.Explicit {
		for _, root := range opts.MailRoots {
			if dir := filepath.Join(root, o.Name); isDir(dir) {
				return dir
			}
		}
		return ""
	}
	if o.Mailbox != "" && isDir(o.Mailbox) {
		return o.Mailbox
	}
	return ""
}

// recordID derives the dlg- id from orch's run identity. orch's run_id is only
// 8 hex (P-6), so the name and creation time are hashed with it.
func recordID(o orchRecord) string {
	return hashID("orch-run", o.Name, o.RunID, strconv.FormatFloat(o.CreatedAt, 'f', -1, 64))
}

func hashID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "dlg-" + hex.EncodeToString(h.Sum(nil))[:16]
}

func dedupe(vs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vs {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
