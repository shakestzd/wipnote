package containerwrite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionVersion is the own session file's format.
const SessionVersion = 1

// Session is a container worker's sessions-ledger row, kept in its own file
// session-<id>.json until the host folds it into the sessions ledger.
// StartedAt is the first start and EndedAt the latest run's end (zero while
// it runs); Runs lists every run once the session has resumed.
type Session struct {
	V         int       `json:"v"`
	SessionID string    `json:"session_id"`
	Harness   string    `json:"harness,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
	Runs      []Run     `json:"runs,omitempty"`
	// Family is the session-family index entry (session id to family id).
	Family string `json:"family,omitempty"`
}

func sessionFile(sid string) (string, error) {
	name := "session-" + strings.TrimSpace(sid) + ".json"
	if sid == "" || !nameRe.MatchString(name) {
		return "", fmt.Errorf("containerwrite: bad session id %q", sid)
	}
	return name, nil
}

// Run is one process lifetime of a session; EndedAt is zero while it runs.
type Run struct {
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
}

// RunList is the session's runs, first to latest: one run for a session
// that never resumed, none before its first start.
func (s Session) RunList() []Run {
	if len(s.Runs) > 0 {
		return append([]Run(nil), s.Runs...)
	}
	if s.StartedAt.IsZero() {
		return nil
	}
	return []Run{{StartedAt: s.StartedAt, EndedAt: s.EndedAt}}
}

func (s *Session) setRuns(runs []Run) {
	s.Runs, s.StartedAt, s.EndedAt = nil, runs[0].StartedAt, runs[len(runs)-1].EndedAt
	if len(runs) > 1 {
		s.Runs = runs
	}
}

// OpenSession records a start. The first start opens run 1; a start after
// the latest run ended is a resume and opens a new run, as the sessions
// ledger's Open does; a start while a run is open is a replay. The first
// real harness is kept.
func OpenSession(wipnoteDir string, s Session) error { return openSession(wipnoteDir, s, true) }

// EnsureSession is the late writer's start, as the sessions ledger's
// Ensure: it records run 1 of a session it has never seen, and otherwise
// only a missing harness. It never starts a run, so a process that outlives
// the session cannot reopen it.
func EnsureSession(wipnoteDir string, s Session) error { return openSession(wipnoteDir, s, false) }

func openSession(wipnoteDir string, s Session, mayResume bool) error {
	name, err := sessionFile(s.SessionID)
	if err != nil {
		return err
	}
	var cur Session
	return withOwnFile(wipnoteDir, name, &cur, func() bool {
		runs, changed := cur.RunList(), cur.SessionID == ""
		cur.SessionID, cur.V = s.SessionID, SessionVersion
		n := len(runs)
		if n == 0 || (mayResume && !runs[n-1].EndedAt.IsZero()) {
			at := s.StartedAt.UTC()
			if n > 0 && at.Before(runs[n-1].EndedAt) {
				at = runs[n-1].EndedAt // runs never overlap
			}
			runs, changed = append(runs, Run{StartedAt: at}), true
			cur.setRuns(runs)
		}
		if cur.Harness == "" && s.Harness != "" {
			cur.Harness, changed = s.Harness, true
		}
		return changed
	})
}

// CloseSession records the end of the latest run, once.
func CloseSession(wipnoteDir, sid string, ended time.Time) error {
	name, err := sessionFile(sid)
	if err != nil {
		return err
	}
	var cur Session
	return withOwnFile(wipnoteDir, name, &cur, func() bool {
		runs := cur.RunList()
		n := len(runs)
		if n == 0 || !runs[n-1].EndedAt.IsZero() {
			return false
		}
		end := ended.UTC()
		if end.Before(runs[n-1].StartedAt) {
			if n > 1 {
				return false // an earlier run's late end, as the sessions ledger reads it
			}
			end = runs[n-1].StartedAt // run 1: clock skew
		}
		runs[n-1].EndedAt = end
		cur.SessionID, cur.V = sid, SessionVersion
		cur.setRuns(runs)
		return true
	})
}

// SetFamily records the session's family id on its own file.
func SetFamily(wipnoteDir, sid, family string) error {
	name, err := sessionFile(sid)
	if err != nil {
		return err
	}
	var cur Session
	return withOwnFile(wipnoteDir, name, &cur, func() bool {
		if cur.Family == family {
			return false
		}
		cur.SessionID, cur.V, cur.Family = sid, SessionVersion, family
		return true
	})
}

// ReadSessions reads every own session file in runDir, oldest start first.
// An unreadable file is skipped: one bad file never hides the others.
func ReadSessions(runDir string) ([]Session, error) {
	var out []Session
	err := EachFile(runDir, "session-", func(_ string, data []byte) {
		var s Session
		if json.Unmarshal(data, &s) == nil && s.SessionID != "" {
			out = append(out, s)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, err
}

// EachFile calls fn with every regular *.json file in runDir whose name
// starts with prefix. A missing folder has no files.
func EachFile(runDir, prefix string, fn func(name string, data []byte)) error {
	entries, err := os.ReadDir(runDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, ".json") {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(runDir, n)); err == nil {
			fn(n, data)
		}
	}
	return nil
}
