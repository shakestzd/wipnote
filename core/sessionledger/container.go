package sessionledger

import (
	"errors"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

// HostContainer is Record.Host for a session that ran in the project's
// devcontainer as a delegate worker.
const HostContainer = "container"

// ErrContainerWorker refuses a ledger repair inside a container worker: it
// writes only its own files, never the shared ledger (plan-28b4a7f6 slice 21).
var ErrContainerWorker = errors.New("sessionledger: a container worker does not write the shared sessions ledger")

// openOwn records a container worker's start in its own file instead of the
// shared ledger; the host folds it in with Host=container. mayResume is
// Open's (a start after the end is a resume); Ensure passes false and never
// starts a run. It reports whether a new row or run was written, as Open
// does.
func (s *Store) openOwn(r Record, mayResume bool) (bool, error) {
	before, _ := ownSession(s.wipnoteDir, r.SessionID)
	write := containerwrite.EnsureSession
	if mayResume {
		write = containerwrite.OpenSession
	}
	err := write(s.wipnoteDir, containerwrite.Session{SessionID: r.SessionID, Harness: r.Harness, StartedAt: r.StartedAt})
	if err != nil {
		return false, err
	}
	after, _ := ownSession(s.wipnoteDir, r.SessionID)
	return len(after.RunList()) > len(before.RunList()), nil
}

func ownSession(wipnoteDir, sid string) (containerwrite.Session, bool) {
	dir, err := containerwrite.RunDir(wipnoteDir)
	if err != nil {
		return containerwrite.Session{}, false
	}
	all, _ := containerwrite.ReadSessions(dir)
	for _, s := range all {
		if s.SessionID == sid {
			return s, true
		}
	}
	return containerwrite.Session{}, false
}
