package importorch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/hostpath"
)

// This file reads orch's files (docs/design/orch-inventory.md sections 2, 6
// and 8). orch owns them; nothing here writes, locks or runs anything in them.

// orchRecord is the part of an orch SessionRecord the import reads. Host
// paths (cwd, pane, launched_by.cwd) are not even decoded, except mailbox,
// which is only used to find the events file.
type orchRecord struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Container  bool    `json:"container"`
	CreatedAt  float64 `json:"created_at"`
	Mailbox    string  `json:"mailbox"`
	RunID      string  `json:"run_id"`
	Mode       string  `json:"mode"`
	WorkItem   string  `json:"work_item"` // orch 0.1.0 never writes it; used only when present
	LaunchedBy *struct {
		Session string `json:"session"`
	} `json:"launched_by"`
}

// loadRegistry returns orch's records sorted by creation time. A missing
// registry is empty (archived mailboxes may still hold runs); a corrupt one or
// an unknown registry version is an error, never "no runs".
func loadRegistry(path string) ([]orchRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read orch registry: %w", pathless(err))
	}
	var reg struct {
		V        *int                  `json:"v"`
		Sessions map[string]orchRecord `json:"sessions"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse orch registry: %w", err)
	}
	if reg.V != nil && *reg.V != 1 {
		return nil, fmt.Errorf("orch registry format v%d is not supported (import-orch reads v1)", *reg.V)
	}
	out := make([]orchRecord, 0, len(reg.Sessions))
	for name, r := range reg.Sessions {
		if r.Name == "" {
			r.Name = name
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// events is what the import keeps from one events.ndjson: the worker session
// ids (first seen first) and the times of the first and last events.
type events struct {
	count       int
	sids        []string
	first, last time.Time
}

// readEvents reads <dir>/events.ndjson in byte order, which is event order for
// both formats: v1 lines carry seq, v2 lines do not (the order is the byte
// offset). A missing or non-regular file gives nothing; a line that is not
// JSON (a torn tail, a hook still appending) is skipped; a line in any format
// but v1 or v2 refuses the whole import, naming the mailbox, not its path.
func readEvents(dir, mailbox string) (events, error) {
	var ev events
	if dir == "" {
		return ev, nil
	}
	path := filepath.Join(dir, "events.ndjson")
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return ev, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ev, fmt.Errorf("orch mailbox %q: read events: %w", mailbox, pathless(err))
	}
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[:i+1]
	} else {
		data = nil
	}
	seen := map[string]bool{}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if err := ev.add(line, seen); err != nil {
			return events{}, fmt.Errorf("orch mailbox %q: %w", mailbox, err)
		}
	}
	return ev, nil
}

func (ev *events) add(line []byte, seen map[string]bool) error {
	var e struct {
		V   json.RawMessage `json:"v"`
		TS  float64         `json:"ts"`
		SID string          `json:"sid"`
	}
	if len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &e) != nil {
		return nil
	}
	switch v := string(e.V); v {
	case "1", "2":
	case "":
		return errors.New("an event line has no format version (import-orch reads v1 and v2)")
	default:
		return fmt.Errorf("event format v%s is not supported (import-orch reads v1 and v2)", v)
	}
	ev.count++
	if e.TS > 0 {
		ts := epoch(e.TS)
		if ev.first.IsZero() {
			ev.first = ts
		}
		ev.last = ts
	}
	if sid := cleanSessionID(e.SID); sid != "" && !seen[sid] {
		seen[sid] = true
		ev.sids = append(ev.sids, sid)
	}
	return nil
}

// cleanSessionID normalises a path-style session id to the bare id, as
// wipnote's hooks store it, and drops any value that still names a host path
// or is not one token.
func cleanSessionID(raw string) string {
	sid := agent.NormaliseSessionID(strings.TrimSpace(raw))
	if sid == "" || strings.ContainsAny(sid, " \t\r\n") || hostpath.Check("sid", sid) != nil {
		return ""
	}
	return sid
}

func epoch(sec float64) time.Time {
	return time.Unix(0, int64(sec*1e9)).UTC()
}

// archiveName is orch's archive folder: <name>-<epoch>-<8 hex token>, written
// at spawn when a name is reused (stores/mailbox.py archive).
var archiveName = regexp.MustCompile(`^([a-z][a-z0-9_-]{0,31})-([0-9]+)-([0-9a-f]{8})$`)

// archived is one previous mailbox under <mail_root>/.archive/.
type archived struct {
	folder, name, dir string
}

// listArchives returns the archived mailboxes under root, in folder order.
// Symlinks are refused, as orch refuses them below its root.
func listArchives(root string) []archived {
	base := filepath.Join(root, ".archive")
	if !isDir(base) {
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []archived
	for _, e := range entries {
		m := archiveName.FindStringSubmatch(e.Name())
		if m == nil || e.Type()&os.ModeSymlink != 0 || !e.IsDir() {
			continue
		}
		out = append(out, archived{folder: e.Name(), name: m[1], dir: filepath.Join(base, e.Name())})
	}
	return out
}

// pathless drops the path from a file error, so a message never prints a
// host path.
func pathless(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// isDir reports a real directory, not a symlink to one.
func isDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// RegistryPath returns $ORCH_REGISTRY, else orch's default
// ~/.cache/orch/registry.json.
func RegistryPath(getenv func(string) string, home string) string {
	if p := getenv("ORCH_REGISTRY"); p != "" {
		return p
	}
	return filepath.Join(home, ".cache", "orch", "registry.json")
}

// MailRoots returns the mail roots to read. A --mail-root flag is the only
// root. Otherwise orch's default moved twice, so every known one is probed:
// $ORCH_MAIL_ROOT, $ORCH_SHARED_ROOT/.orch/mail, ~/work/.orch/mail and the
// older ~/work/adhoc/orch-mail. Import also reads the root each registry
// record's mailbox sits in.
func MailRoots(flag string, getenv func(string) string, home string) []string {
	if flag != "" {
		return []string{flag}
	}
	var roots []string
	if p := getenv("ORCH_MAIL_ROOT"); p != "" {
		roots = append(roots, p)
	}
	if p := getenv("ORCH_SHARED_ROOT"); p != "" {
		roots = append(roots, filepath.Join(p, ".orch", "mail"))
	}
	roots = append(roots,
		filepath.Join(home, "work", ".orch", "mail"),
		filepath.Join(home, "work", "adhoc", "orch-mail"))
	return dedupe(roots)
}
