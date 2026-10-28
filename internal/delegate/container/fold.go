package container

import (
	"errors"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/containerwrite"
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

// Fold folds a container worker's own files in runDir into the host's
// sessions ledger (host=container, the worker's real harness), the session
// family index and the gate ledger. It is idempotent: a row already there is
// only closed once, and a gate run already recorded is not appended again.
func Fold(wipnoteDir, runDir string) (Folded, error) {
	if containerwrite.Active() {
		return Folded{}, ErrFoldInside
	}
	var f Folded
	sessions, err := containerwrite.ReadSessions(runDir)
	if err != nil {
		return f, err
	}
	for _, s := range sessions {
		if err := foldSession(wipnoteDir, s, &f); err != nil {
			return f, err
		}
	}
	return f, foldGates(wipnoteDir, runDir, &f)
}

// foldSession folds the worker's runs in order: a run the host's row lacks
// is opened (the first one creates the row, a later one is a resume, as the
// sessions ledger's Open records it), and the latest run's end is closed
// once. Runs are matched by position, so folding the same file again finds
// every run already there and writes nothing.
func foldSession(wipnoteDir string, s containerwrite.Session, f *Folded) error {
	store := sessionledger.NewStore(wipnoteDir)
	row, found, err := store.Get(s.SessionID)
	if err != nil {
		return err
	}
	var host []sessionledger.Run
	if found {
		host = row.RunList()
	}
	for i, run := range s.RunList() {
		if err := foldOneRun(store, wipnoteDir, s, i, run, &host, f); err != nil {
			return err
		}
	}
	return foldFamily(wipnoteDir, s, f)
}

func foldOneRun(store *sessionledger.Store, wipnoteDir string, s containerwrite.Session, i int, run containerwrite.Run,
	host *[]sessionledger.Run, f *Folded) error {
	if i >= len(*host) {
		rec := sessionledger.Record{SessionID: s.SessionID, Harness: s.Harness, StartedAt: run.StartedAt,
			ProjectDir: paths.NormalizeProjectDir(filepath.Dir(wipnoteDir)), Host: sessionledger.HostContainer}
		if _, err := store.Open(rec); err != nil {
			return err
		}
		if i == 0 {
			f.Sessions++
		} else {
			f.Resumed++
		}
		*host = append(*host, sessionledger.Run{StartedAt: run.StartedAt})
	}
	latest := i == len(*host)-1
	if run.EndedAt.IsZero() || !latest || !(*host)[i].IsOpen() {
		return nil
	}
	if err := store.Close(s.SessionID, run.EndedAt); err != nil {
		return err
	}
	(*host)[i].EndedAt = run.EndedAt
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

func foldGates(wipnoteDir, runDir string, f *Folded) error {
	own, err := gateledger.ReadOwn(runDir)
	if err != nil || len(own) == 0 {
		return err
	}
	store := gateledger.NewStore(wipnoteDir)
	have, err := store.ReadAll()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, r := range have {
		seen[r.ID] = true
	}
	for _, r := range own {
		if seen[r.ID] {
			continue
		}
		if _, err := store.Append(r); err != nil {
			return err
		}
		f.Gates++
	}
	return nil
}
