// Package orchlink reads the files of orch, the herdr-pane session driver, so
// wipnote can show orch sessions next to the work items they serve.
//
// orch owns its files and is their only writer: registry.json (one record per
// session name) and <mailbox>/events.ndjson (one line per hook event). This
// package only reads them, without orch's lock, and never copies them into
// .wipnote/. See docs/design/orch-integration.md.
package orchlink

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// EnvRegistry names the registry path; the same variable orch reads.
const EnvRegistry = "ORCH_REGISTRY"

// eventTailBytes bounds how much of an event log is read, like orch's
// ORCH_EVENT_TAIL_BYTES default.
const eventTailBytes = 1 << 20

// Link says why a session is shown under a work item.
const (
	LinkTag = "tag" // orch spawn --work-item named the work item
	LinkSID = "sid" // derived: a harness session id in its events is a session of the work item
)

// Session is one orch session as wipnote shows it. No host paths.
type Session struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	State         string   `json:"state"`
	Container     bool     `json:"container"`
	WorkItem      string   `json:"work_item,omitempty"`
	Link          string   `json:"link"`
	SessionIDs    []string `json:"session_ids,omitempty"`
	Subagents     int      `json:"subagents"`
	CallerSession string   `json:"caller_session,omitempty"`
}

// record is the part of an orch registry record this package reads.
type record struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Container  bool   `json:"container"`
	State      string `json:"state"`
	CreatedAt  int64  `json:"created_at"`
	Mailbox    string `json:"mailbox"`
	WorkItem   string `json:"work_item"`
	LaunchedBy *struct {
		Session string `json:"session"`
	} `json:"launched_by"`
}

// RegistryPath returns $ORCH_REGISTRY, else ~/.cache/orch/registry.json (orch's default).
func RegistryPath(getenv func(string) string, home string) string {
	if p := getenv(EnvRegistry); p != "" {
		return p
	}
	return filepath.Join(home, ".cache", "orch", "registry.json")
}

// loadRegistry returns the records sorted by creation time. A missing file
// means orch is not in use and returns (nil, nil); any other failure is an
// error, so an unreadable registry never reads as "no sessions".
func loadRegistry(path string) ([]record, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read orch registry: %w", err)
	}
	var reg struct {
		Sessions map[string]record `json:"sessions"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse orch registry: %w", err)
	}
	out := make([]record, 0, len(reg.Sessions))
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

// ForWorkItem returns the orch sessions that serve workItem: those tagged with
// it, and untagged ones whose events carry a session id in workItemSessions
// (the work item's own sessions). A session tagged with another work item is
// never derived onto this one.
func ForWorkItem(registryPath, workItem string, workItemSessions map[string]bool) ([]Session, error) {
	recs, err := loadRegistry(registryPath)
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, r := range recs {
		if r.WorkItem != "" && r.WorkItem != workItem {
			continue
		}
		sids, subagents := readEvents(r.Mailbox)
		link := LinkTag
		if r.WorkItem == "" {
			if !anyIn(sids, workItemSessions) {
				continue
			}
			link = LinkSID
		}
		out = append(out, toSession(r, link, sids, subagents))
	}
	return out, nil
}

func toSession(r record, link string, sids []string, subagents int) Session {
	s := Session{
		Name: r.Name, Kind: r.Kind, State: r.State, Container: r.Container,
		WorkItem: r.WorkItem, Link: link, SessionIDs: sids, Subagents: subagents,
	}
	if r.LaunchedBy != nil {
		s.CallerSession = r.LaunchedBy.Session
	}
	return s
}

func anyIn(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if set[id] {
			return true
		}
	}
	return false
}

// readEvents returns the distinct harness session ids (first seen first) and
// the number of distinct sub-agents started, from the tail of
// <mailbox>/events.ndjson. Best-effort: a missing, non-regular or unreadable
// log gives nothing, a partial last line (a hook still appending) and any
// malformed line are skipped.
func readEvents(mailbox string) ([]string, int) {
	if mailbox == "" {
		return nil, 0
	}
	data := readTail(filepath.Join(mailbox, "events.ndjson"), eventTailBytes)
	var sids []string
	seen := map[string]bool{}
	agents := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), eventTailBytes)
	for sc.Scan() {
		var ev struct {
			Kind    string `json:"kind"`
			SID     string `json:"sid"`
			AgentID string `json:"agent_id"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.SID != "" && !seen[ev.SID] {
			seen[ev.SID] = true
			sids = append(sids, ev.SID)
		}
		if ev.Kind == "subagent_start" && ev.AgentID != "" {
			agents[ev.AgentID] = true
		}
	}
	return sids, len(agents)
}

// readTail returns the complete lines among the last max bytes of a regular
// file: a line cut by the window start and a last line with no newline yet
// are dropped.
func readTail(path string, max int64) []byte {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	start := info.Size() - max
	if start < 0 {
		start = 0
	}
	data, err := io.ReadAll(io.NewSectionReader(f, start, info.Size()-start))
	if err != nil {
		return nil
	}
	if start > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			return nil
		}
	}
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		return data[:i+1]
	}
	return nil
}
