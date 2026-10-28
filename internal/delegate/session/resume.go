package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// Resume (plan-28b4a7f6 slice 20, q-resume-mode option A; design note
// docs/design/delegate-resume.md): spawn NAME2 --resume NAME starts a new
// worker that continues NAME's last harness session, in NAME's recorded cwd
// and place. It refuses, before anything is written, a live NAME, a swept run
// folder and a run with no recorded session id. The new run is its own
// ledger row with resumed_from set; the old row stays closed.

// ErrCannotResume is every resume refusal. Test it with errors.Is.
var ErrCannotResume = errors.New("delegate: cannot resume")

// sessionToken is a harness session id that is safe on the launcher argv (it
// cannot read as a flag); the bind hook accepts the same shape.
var sessionToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// Resume fills o from the stopped run named from (harness, cwd, place, work
// item when o has none) and spawns it with --resume <last session id>.
func Resume(ctx context.Context, d Deps, o SpawnOptions, from string) (Spawned, error) {
	if d.Herdr == nil {
		return Spawned{}, ErrNotInHerdr
	}
	if err := resumeOptions(d, &o, from); err != nil {
		return Spawned{}, err
	}
	return Spawn(ctx, d, o)
}

func resumeOptions(d Deps, o *SpawnOptions, from string) error {
	old, err := lastRun(d, from)
	if err != nil {
		return err
	}
	if err := checkLaunched(d, old); err != nil {
		return err
	}
	info, sid, err := resumeSource(d, old)
	if err != nil {
		return err
	}
	if err := matchResume(*o, old, info); err != nil {
		return err
	}
	o.Harness, o.Cwd, o.ResumedFrom, o.ResumeSession = old.Harness, info.Cwd, old.ID, sid
	if o.WorkItem == "" {
		o.WorkItem = old.WorkItem
	}
	if info.Place == PlaceContainer {
		o.Container, o.Root = true, info.Root
	}
	return nil
}

// lastRun is the newest interactive row named name; it must be closed.
func lastRun(d Deps, name string) (delegationledger.Record, error) {
	recs, err := d.Ledger.ReadAll()
	if err != nil {
		return delegationledger.Record{}, fmt.Errorf("%w: the delegations ledger cannot be read: %v", ErrCannotResume, err)
	}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Mode != delegationledger.ModeInteractive || r.Name != name {
			continue
		}
		if r.IsOpen() {
			return r, fmt.Errorf("%w: %s is live (run %s); stop it first, or run \"wipnote delegate list\" to close a run whose pane is gone",
				ErrCannotResume, name, r.ID)
		}
		return r, nil
	}
	return delegationledger.Record{}, fmt.Errorf("%w: no worker named %q is on record", ErrCannotResume, name)
}

// resumeSource reads the old run folder (cwd, place) and the last recorded
// session id: the ledger's worker_sessions plus any bind events a list had
// not yet moved there.
func resumeSource(d Deps, old delegationledger.Record) (Info, string, error) {
	rf, info, err := loadInfo(d.Store, old.ID)
	if err != nil {
		return Info{}, "", fmt.Errorf("%w: run %s's folder was swept or is unreadable, and its cwd and place are recorded only there (%v)",
			ErrCannotResume, old.ID, err)
	}
	evs, _ := rf.ReadEvents()
	sessions := mergeSessions(old.WorkerSessions, factsOf(evs).sessions)
	if len(sessions) == 0 && info.ResumeSession != "" {
		// A resumed run stopped before its bind was seen still continues
		// the session it was started on.
		sessions = []string{info.ResumeSession}
	}
	if len(sessions) == 0 {
		return Info{}, "", fmt.Errorf("%w: run %s has no recorded harness session id (its SessionStart hook never bound one), so there is nothing to resume",
			ErrCannotResume, old.ID)
	}
	sid := sessions[len(sessions)-1]
	if !sessionToken.MatchString(sid) {
		return Info{}, "", fmt.Errorf("%w: run %s's session id %q is not a plain id", ErrCannotResume, old.ID, sid)
	}
	if fi, err := os.Stat(info.Cwd); info.Cwd == "" || err != nil || !fi.IsDir() {
		return Info{}, "", fmt.Errorf("%w: run %s's working folder %q is gone; the session resumes only in the folder it ran in",
			ErrCannotResume, old.ID, info.Cwd)
	}
	return info, sid, nil
}

// matchResume refuses flags that disagree with the recorded run. A Codex
// resume takes no brief: codex resume's first prompt is unverified.
func matchResume(o SpawnOptions, old delegationledger.Record, info Info) error {
	switch {
	case o.Harness != "" && o.Harness != old.Harness:
		return fmt.Errorf("%w: run %s ran under harness %s, not %s; drop --harness", ErrCannotResume, old.ID, old.Harness, o.Harness)
	case o.Container && info.Place != PlaceContainer:
		return fmt.Errorf("%w: run %s ran on the host; drop --container (a resume runs where the session ran)", ErrCannotResume, old.ID)
	case info.Place == PlaceContainer && o.Root != "" && o.Root != info.Root:
		return fmt.Errorf("%w: run %s ran in another project's devcontainer", ErrCannotResume, old.ID)
	case old.Harness == HarnessCodex && o.Brief != "":
		return fmt.Errorf("%w: a Codex resume takes no brief (codex resume's first prompt is unverified); resume without one, then give the task with \"wipnote delegate send\"",
			ErrCannotResume)
	}
	return nil
}

// checkLaunched refuses resuming from an attempt that was abandoned before
// launch: it never ran, so it is no source of its own. It names the run it
// was resuming, which is the one to resume.
func checkLaunched(d Deps, old delegationledger.Record) error {
	if old.ResumedFrom == "" || !neverLaunched(d, old) {
		return nil
	}
	origin := old.ResumedFrom
	if r, ok, err := d.Ledger.Get(origin); err == nil && ok {
		origin = r.Name + " (run " + r.ID + ")"
	}
	return fmt.Errorf("%w: run %s (%s) was abandoned before its harness started; resume %s instead",
		ErrCannotResume, old.ID, old.Name, origin)
}

// checkSessionFree refuses, under namesLock, a resume of session sid from
// run from when a newer run, or any open one, already continues sid: one
// harness session is continued by one run at a time, whatever the chain.
// The source must be the session's newest holder. An attempt abandoned
// before launch never counts (neverLaunched).
func checkSessionFree(d Deps, from, sid string) error {
	if from == "" {
		return nil
	}
	recs, err := d.Ledger.ReadAll()
	if err != nil {
		return fmt.Errorf("%w: the delegations ledger cannot be read: %v", ErrCannotResume, err)
	}
	after := false
	for _, r := range recs {
		if r.ID == from {
			after = true
			continue
		}
		if (!after && !r.IsOpen()) || r.Mode != delegationledger.ModeInteractive || neverLaunched(d, r) {
			continue
		}
		if slices.Contains(runSessions(d, r), sid) {
			return fmt.Errorf("%w: session %s is already continued by run %s (%s); resume that one instead", ErrCannotResume, sid, r.ID, r.Name)
		}
	}
	return nil
}

// runSessions is every session a run holds or continues: the ledger's
// worker_sessions, its bind events and its recorded resume_session.
func runSessions(d Deps, r delegationledger.Record) []string {
	out := r.WorkerSessions
	if rf, info, err := loadInfo(d.Store, r.ID); err == nil {
		evs, _ := rf.ReadEvents()
		out = mergeSessions(out, factsOf(evs).sessions)
		if info.ResumeSession != "" {
			out = mergeSessions(out, []string{info.ResumeSession})
		}
	}
	return out
}

// neverLaunched reports a resume attempt that spawn abandoned before the
// harness started: abandon closes the row with end_reason gone, and no
// session was ever bound (none in the ledger, no session_start in its run
// folder). A run folder that is gone gives no proof, so the run counts.
func neverLaunched(d Deps, r delegationledger.Record) bool {
	if r.IsOpen() || r.EndReason != delegationledger.EndGone || len(r.WorkerSessions) > 0 {
		return false
	}
	rf, err := d.Store.Run(r.ID)
	if err != nil {
		return false
	}
	evs, err := rf.ReadEvents()
	return err == nil && len(factsOf(evs).sessions) == 0
}
