package session

import (
	"encoding/json"
	"fmt"

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
	// TerminalID is pinned by the first list or wait that sees the harness
	// (adopt a late agent); empty until then.
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

// WriteInfo replaces the run folder's session.json atomically.
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
