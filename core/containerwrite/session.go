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
type Session struct {
	V         int       `json:"v"`
	SessionID string    `json:"session_id"`
	Harness   string    `json:"harness,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
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

// OpenSession records the session's start. The first start and the first
// real harness are kept; a replayed start changes nothing.
func OpenSession(wipnoteDir string, s Session) error {
	name, err := sessionFile(s.SessionID)
	if err != nil {
		return err
	}
	var cur Session
	return withOwnFile(wipnoteDir, name, &cur, func() bool {
		changed := cur.SessionID == ""
		if changed {
			cur.SessionID = s.SessionID
		}
		if cur.StartedAt.IsZero() {
			cur.StartedAt, changed = s.StartedAt.UTC(), true
		}
		if cur.Harness == "" && s.Harness != "" {
			cur.Harness, changed = s.Harness, true
		}
		cur.V = SessionVersion
		return changed
	})
}

// CloseSession records the session's first end.
func CloseSession(wipnoteDir, sid string, ended time.Time) error {
	name, err := sessionFile(sid)
	if err != nil {
		return err
	}
	var cur Session
	return withOwnFile(wipnoteDir, name, &cur, func() bool {
		if !cur.EndedAt.IsZero() {
			return false
		}
		cur.SessionID, cur.V, cur.EndedAt = sid, SessionVersion, ended.UTC()
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
