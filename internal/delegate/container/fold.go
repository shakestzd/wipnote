package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/gateledger"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// Folded counts what one fold wrote: new session rows, resumed runs, ends,
// gate runs and family entries. A fold that finds nothing new writes nothing.
type Folded struct{ Sessions, Resumed, Ended, Gates, Families, Claims, ClaimCloses int }

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
	// ClaimCloses are the worker's pending claim closes already applied.
	ClaimCloses map[string]bool `json:"claim_closes,omitempty"`
	// FoldedAt is when the last fold that found anything to fold began: an
	// own file written after it is unfolded (Unfolded).
	FoldedAt time.Time `json:"folded_at,omitzero"`
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
	return f, foldLocked(wipnoteDir, runDir, &f)
}

// foldLocked reads the worker's session files, claim files, gate runs and
// folded.json under the fold lock, so a fold never works from a snapshot
// another fold has already moved past, and folds them. FoldedAt advances
// only when every part succeeded, so a part that failed (a gate append
// under a transient lock or IO error) leaves the folder unfolded and
// FoldPending retries it; what did fold is recorded and not folded again.
func foldLocked(wipnoteDir, runDir string, f *Folded) error {
	path := filepath.Join(runDir, FoldedFile)
	// One project-wide lock for every fold of sessions and claims: two folds
	// of one run re-read folded.json under it, and two run folders of one
	// resumed session never interleave on the host's row.
	if foldBeforeLock != nil {
		foldBeforeLock()
	}
	release := filelock.Guard(filepath.Join(wipnoteDir, "delegate", foldLock))
	defer release()
	start := foldNow().UTC()
	st := readFolded(path)
	stale := unfoldedSince(runDir, st.FoldedAt)
	before := fmt.Sprint(st)
	ferr := foldSessionFiles(wipnoteDir, runDir, &st, f)
	if ferr == nil {
		ferr = foldClaims(wipnoteDir, runDir, &st, f)
	}
	if ferr == nil {
		ferr = foldGates(wipnoteDir, runDir, f)
	}
	if ferr == nil && stale {
		st.FoldedAt = start
	}
	if fmt.Sprint(st) != before {
		data, _ := json.MarshalIndent(st, "", "  ")
		if err := filelock.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil && ferr == nil {
			ferr = err
		}
	}
	return ferr
}

func readFolded(path string) foldedState {
	var st foldedState
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	if st.Sessions == nil {
		st.Sessions = map[string]foldedSession{}
	}
	if st.ClaimCloses == nil {
		st.ClaimCloses = map[string]bool{}
	}
	return st
}

func foldSessionFiles(wipnoteDir, runDir string, st *foldedState, f *Folded) error {
	sessions, err := containerwrite.ReadSessions(runDir)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		fs := st.Sessions[s.SessionID]
		err := foldSession(wipnoteDir, s, &fs, f)
		st.Sessions[s.SessionID] = fs
		if err != nil {
			return err
		}
	}
	return nil
}

// foldClaims merges the worker's own claim shards into the shared ones and
// applies each of its pending closes once (folded.json records them), each
// under the shard's lock in the claim store.
func foldClaims(wipnoteDir, runDir string, st *foldedState, f *Folded) error {
	store := claimledger.NewStore(wipnoteDir)
	shards, err := claimledger.OwnShards(runDir)
	if err != nil {
		return err
	}
	for _, path := range shards {
		eps, err := claimledger.ReadFile(path)
		if err != nil {
			return err
		}
		n, err := store.MergeOwnShard(filepath.Base(path), eps)
		if err != nil {
			return err
		}
		f.Claims += n
	}
	ops, err := claimledger.ReadPendingCloses(runDir)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if st.ClaimCloses[op.ID] {
			continue
		}
		if _, err := store.CloseWorkItem(op.WorkItemID, op.Outcome, op.EndedAt, op.StartedBy); err != nil {
			return err
		}
		st.ClaimCloses[op.ID] = true
		f.ClaimCloses++
	}
	return nil
}

// foldSession opens, in order, each of the session's runs this folder has
// not folded yet (the first creates the row, a later one, or the first of a
// resumed run's folder, resumes it, as the sessions ledger's Open records
// it), closing the run before each one, then closes the latest run's end
// once.
func foldSession(wipnoteDir string, s containerwrite.Session, fs *foldedSession, f *Folded) error {
	store := sessionledger.NewStore(wipnoteDir)
	runs := s.RunList()
	if len(runs) < fs.Runs {
		return nil // an older snapshot than what was folded: nothing new
	}
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
	if found && hasRun(row, run.StartedAt) {
		return nil // already in the ledger (folded.json is only the fold's note)
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
	if row, found, err := store.Get(sid); err != nil {
		return err
	} else if found && !row.IsOpen() {
		fs.Ended = true // the ledger already holds the end
		return nil
	}
	if err := store.Close(sid, run.EndedAt); err != nil {
		return err
	}
	fs.Ended = true
	f.Ended++
	return nil
}

// foldFamily registers the session's family in the shared index when the
// index lacks it or holds another. It runs under the fold lock.
func foldFamily(wipnoteDir string, s containerwrite.Session, f *Folded) error {
	project := filepath.Dir(wipnoteDir)
	if s.Family == "" {
		return nil
	}
	// The shared index is the record written here, so it is what is
	// checked: SessionFamilyFor prefers the per-session state file, which
	// the worker's own SessionStart wrote, and would hide a missing entry.
	idx, err := agent.ReadSessionFamilyIndex(project)
	if err != nil {
		return err
	}
	if idx[s.SessionID] == s.Family {
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
		if foldGateErr != nil {
			if err := foldGateErr(); err != nil {
				return err
			}
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

// foldGateErr is a test seam: a non-nil error it returns fails the gate
// append, as a transient gate-ledger lock or IO error would.
var foldGateErr func() error

// foldGateCheck is a test seam: called just before a fold records a gate
// run, where an unlocked check-then-append would let another fold in.
var foldGateCheck func()

// foldBeforeLock is a test seam: called just before a session fold takes the
// project-wide fold lock, where another fold may run first.
var foldBeforeLock func()

// hasRun reports whether the ledger row already holds a run started at.
func hasRun(row sessionledger.Record, at time.Time) bool {
	for _, r := range row.RunList() {
		if r.StartedAt.Equal(at) {
			return true
		}
	}
	return false
}
