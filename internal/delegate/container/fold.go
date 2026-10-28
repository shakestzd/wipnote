package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/gateledger"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// Folded counts what one fold wrote: new session rows, resumed runs, ends,
// gate runs and family entries. A fold that finds nothing new writes nothing.
type Folded struct{ Sessions, Resumed, Ended, Gates, Families int }

// ErrFoldInside refuses a fold inside a container worker: only the host
// writes the shared ledgers.
var ErrFoldInside = errors.New("delegate: only the host folds a container worker's files")

// FoldedFile is the host's own record, in the run folder, of what it has
// folded from that folder: per session, how many of its runs were opened in
// the host's ledger and whether the latest one's end was closed. The host
// alone writes it (replaced by rename); the container never does.
const FoldedFile = "folded.json"

// foldLock names the host's fold lock in .wipnote/delegate/ (its sidecar is
// fold.lock).
const foldLock = "fold"

type foldedSession struct {
	Runs  int  `json:"runs"`
	Ended bool `json:"ended"`
}

type foldedState struct {
	Sessions map[string]foldedSession `json:"sessions"`
}

// Fold folds a container worker's own files in runDir into the host's
// sessions ledger (host=container, the worker's real harness), the session
// family index and the gate ledger. It is idempotent: what the run folder's
// folded.json says was folded is not folded again, and a gate run already
// recorded is not appended again. A resumed run is a new run folder whose
// file holds only its own runs of the session; those are added to the
// host's row as resumed runs.
func Fold(wipnoteDir, runDir string) (Folded, error) {
	if containerwrite.Active() {
		return Folded{}, ErrFoldInside
	}
	var f Folded
	sessions, err := containerwrite.ReadSessions(runDir)
	if err != nil {
		return f, err
	}
	if len(sessions) > 0 {
		if err := foldSessions(wipnoteDir, runDir, sessions, &f); err != nil {
			return f, err
		}
	}
	return f, foldGates(wipnoteDir, runDir, &f)
}

func foldSessions(wipnoteDir, runDir string, sessions []containerwrite.Session, f *Folded) error {
	path := filepath.Join(runDir, FoldedFile)
	// One project-wide lock for every session fold: two folds of one run
	// re-read folded.json under it, and two run folders of one resumed
	// session (an end and a resume) never interleave on the host's row.
	release := filelock.Guard(filepath.Join(wipnoteDir, "delegate", foldLock))
	defer release()
	st := foldedState{Sessions: map[string]foldedSession{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
		if st.Sessions == nil {
			st.Sessions = map[string]foldedSession{}
		}
	}
	before := fmt.Sprint(st.Sessions)
	var ferr error
	for _, s := range sessions {
		fs := st.Sessions[s.SessionID]
		ferr = foldSession(wipnoteDir, s, &fs, f)
		st.Sessions[s.SessionID] = fs
		if ferr != nil {
			break
		}
	}
	if fmt.Sprint(st.Sessions) != before {
		data, _ := json.MarshalIndent(st, "", "  ")
		if err := filelock.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil && ferr == nil {
			ferr = err
		}
	}
	return ferr
}

// foldSession opens, in order, each of the session's runs this folder has
// not folded yet (the first creates the row, a later one, or the first of a
// resumed run's folder, resumes it, as the sessions ledger's Open records
// it), closing the run before each one, then closes the latest run's end
// once.
func foldSession(wipnoteDir string, s containerwrite.Session, fs *foldedSession, f *Folded) error {
	store := sessionledger.NewStore(wipnoteDir)
	runs := s.RunList()
	for i := fs.Runs; i < len(runs); i++ {
		if i > 0 && !fs.Ended {
			if err := closeRun(store, s.SessionID, runs[i-1], fs, f); err != nil {
				return err
			}
		}
		if err := openRun(store, wipnoteDir, s, runs[i], f); err != nil {
			return err
		}
		fs.Runs, fs.Ended = i+1, false
	}
	if fs.Runs > 0 && !fs.Ended {
		if err := closeRun(store, s.SessionID, runs[fs.Runs-1], fs, f); err != nil {
			return err
		}
	}
	return foldFamily(wipnoteDir, s, f)
}

func openRun(store *sessionledger.Store, wipnoteDir string, s containerwrite.Session, run containerwrite.Run, f *Folded) error {
	row, found, err := store.Get(s.SessionID)
	if err != nil {
		return err
	}
	rec := sessionledger.Record{SessionID: s.SessionID, Harness: s.Harness, StartedAt: run.StartedAt,
		ProjectDir: paths.NormalizeProjectDir(filepath.Dir(wipnoteDir)), Host: sessionledger.HostContainer}
	if _, err := store.Open(rec); err != nil {
		return err
	}
	switch {
	case !found:
		f.Sessions++
	case !row.IsOpen():
		f.Resumed++
	}
	return nil
}

// closeRun records run's end, when it has one, on the host's latest run.
func closeRun(store *sessionledger.Store, sid string, run containerwrite.Run, fs *foldedSession, f *Folded) error {
	if run.EndedAt.IsZero() {
		return nil
	}
	if err := store.Close(sid, run.EndedAt); err != nil {
		return err
	}
	fs.Ended = true
	f.Ended++
	return nil
}

func foldFamily(wipnoteDir string, s containerwrite.Session, f *Folded) error {
	project := filepath.Dir(wipnoteDir)
	if s.Family == "" || agent.SessionFamilyFor(project, s.SessionID) == s.Family {
		return nil
	}
	if err := agent.RegisterSessionFamily(project, s.SessionID, s.Family); err != nil {
		return err
	}
	f.Families++
	return nil
}

// foldGates records each of the worker's gate runs the host's ledger does
// not hold yet. The check and the append are one step under the ledger
// lock (AppendIfAbsent), so concurrent folds of one run record it once.
func foldGates(wipnoteDir, runDir string, f *Folded) error {
	own, err := gateledger.ReadOwn(runDir)
	if err != nil || len(own) == 0 {
		return err
	}
	store := gateledger.NewStore(wipnoteDir)
	for _, r := range own {
		if foldGateCheck != nil {
			foldGateCheck()
		}
		_, added, err := store.AppendIfAbsent(r)
		if err != nil {
			return err
		}
		if added {
			f.Gates++
		}
	}
	return nil
}

// foldGateCheck is a test seam: called just before a fold records a gate
// run, where an unlocked check-then-append would let another fold in.
var foldGateCheck func()
