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
// shared ledger; the host folds it in with Host=container. It reports a new
// row the first time only, as Open does.
func (s *Store) openOwn(r Record) (bool, error) {
	before, _ := ownSession(s.wipnoteDir, r.SessionID)
	err := containerwrite.OpenSession(s.wipnoteDir, containerwrite.Session{
		SessionID: r.SessionID, Harness: r.Harness, StartedAt: r.StartedAt,
	})
	return err == nil && before.StartedAt.IsZero(), err
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
