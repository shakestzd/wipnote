package session

import (
	"fmt"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/container"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// closeWorkerRow is the one close of an interactive worker's row: a
// container worker's own files (its session and gate records) are folded
// into the host's ledgers first, then the row is closed if it is still
// open. It reports whether this call closed it, so a caller can finish the
// run folder or expire claims only then (fold, close-if-open, finish). No
// caller closes a row without the fold, so a worker lost to wait, list or
// stop never leaves its records unfolded (slice 21).
func closeWorkerRow(d Deps, run, reason string) (bool, error) {
	if rf, info, err := loadInfo(d.Store, run); err == nil {
		foldContainer(d, rf, info)
	}
	return d.Ledger.CloseIfOpen(run, delegationledger.CloseInfo{Ended: d.now(), Reason: reason})
}

// WorkerPreamble is TaskPreamble (the claim line, then the result preamble)
// for a task to info's worker, naming the mailbox paths that worker sees:
// the container's workspace folder for a container worker, the host's
// otherwise. Every message that tells a worker where to write (the brief,
// each sent task) is built here.
func WorkerPreamble(store *runstore.Store, info Info) (string, error) {
	dir, err := workerMailDir(store, info)
	if err != nil {
		return "", err
	}
	return TaskPreamble(info, dir), nil
}

// workerMailDir is the worker's mailbox as the worker sees it. A container
// worker's workspace folder is known once its boot ran devcontainer up;
// before that nothing is sent, rather than a host path it cannot write.
func workerMailDir(store *runstore.Store, info Info) (string, error) {
	mb, err := store.Mailbox(info.Name)
	if err != nil {
		return "", err
	}
	if info.Place != PlaceContainer {
		return mb.Dir(), nil
	}
	if info.Workspace == "" {
		return "", fmt.Errorf("delegate: worker %s's container workspace folder is not known yet (its boot has not finished devcontainer up); nothing was sent", info.Name)
	}
	return container.Mapper{Root: info.Root, Remote: info.Workspace}.Path(mb.Dir())
}
