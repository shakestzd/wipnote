package session

import (
	"encoding/json"
	"fmt"
	"github.com/shakestzd/wipnote/core/filelock"
	"path/filepath"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Place values: where the worker runs.
const (
	PlaceHost      = "host"
	PlaceContainer = "container"
)

// Info is a worker's run-folder record (session.json). It holds host-local
// facts the ledger must not: the pane, the caller's pane and the cwd.
type Info struct {
	Run        string `json:"run"`
	Name       string `json:"name"`
	Harness    string `json:"harness"`
	WorkItem   string `json:"work_item"`
	Caller     string `json:"caller_session,omitempty"`
	CallerPane string `json:"caller_pane,omitempty"`
	Pane       string `json:"pane,omitempty"`
	// TerminalID is the pane's terminal, pinned at spawn right after the
	// pane is placed (an older run gets it when list adopts its agent). Every
	// key typed and every close checks it (ownPane); empty means unknown.
	TerminalID string `json:"terminal_id,omitempty"`
	Renamed    bool   `json:"renamed,omitempty"`
	Cwd        string `json:"cwd"`
	Place      string `json:"place"`
	// Task is the brief's task message id: its result file is
	// mail/<name>/outbox/<task>-result.json. Empty without a brief.
	Task string `json:"task,omitempty"`
	// Root and Workspace are a container worker's project root (the
	// --workspace-folder) and the container's workspace folder (from
	// devcontainer up, set by the boot).
	Root      string `json:"root,omitempty"`
	Workspace string `json:"workspace_folder,omitempty"`
	// ResumedFrom and ResumeSession: a resumed worker's old run and the
	// harness session boot passes to --resume (slice 20).
	ResumedFrom   string `json:"resumed_from,omitempty"`
	ResumeSession string `json:"resume_session,omitempty"`
	Created       string `json:"created"`
}

// Pin is the herdr pin of the worker's agent (pane plus pinned terminal).
func (i Info) Pin() herdr.Pin {
	return herdr.Pin{Pane: herdr.PaneID(i.Pane), TerminalID: i.TerminalID}
}

// UpdateInfo is the one read-modify-write of session.json: under the run's
// session lock it re-reads the file, lets set change only the fields its
// caller owns (spawn: Pane; adoption: TerminalID, Renamed; the boot:
// Workspace), and replaces the file by rename. It returns the info written.
// Every writer of session.json runs on the host, where the lock holds.
func UpdateInfo(rf *runstore.RunFolder, set func(*Info)) (Info, error) {
	release := filelock.Guard(filepath.Join(rf.Dir(), runstore.FileSession))
	defer release()
	info, err := ReadInfo(rf)
	if err != nil {
		return Info{}, err
	}
	set(&info)
	return info, WriteInfo(rf, info)
}

// WriteInfo replaces the run folder's session.json atomically. It writes
// the whole record, so it is for creating the file only; a change to an
// existing one goes through UpdateInfo.
func WriteInfo(rf *runstore.RunFolder, info Info) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return rf.WriteFile(runstore.FileSession, append(data, '\n'))
}

// ReadInfo reads the run folder's session.json.
func ReadInfo(rf *runstore.RunFolder) (Info, error) {
	data, _, err := rf.ReadFrom(runstore.FileSession, 0)
	if err != nil {
		return Info{}, fmt.Errorf("delegate: run %s: no %s: %w", rf.ID(), runstore.FileSession, err)
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, fmt.Errorf("delegate: run %s: unreadable %s: %w", rf.ID(), runstore.FileSession, err)
	}
	return info, nil
}

// loadInfo opens a run folder and reads its Info.
func loadInfo(store *runstore.Store, runID string) (*runstore.RunFolder, Info, error) {
	rf, err := store.Run(runID)
	if err != nil {
		return nil, Info{}, err
	}
	info, err := ReadInfo(rf)
	return rf, info, err
}

// infoLoaded is a test seam: called with the run id after list or send
// loaded session.json and before they save an adoption.
var infoLoaded func(run string)

func onInfoLoaded(run string) {
	if infoLoaded != nil {
		infoLoaded(run)
	}
}
