package session

import (
	"encoding/json"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Observed is a worker's state.json: its readiness as list last observed it
// and that state's group. Only list writes it (each write replaces it by
// rename); the band and tree read it while the run is open, and ignore it
// once the ledger row is closed.
type Observed struct {
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"`
	Group  Group  `json:"group"`
}

// writeObserved keeps rd in the run folder; a failed write only loses the
// band's copy, so it is not reported.
func writeObserved(rf *runstore.RunFolder, rd Readiness) {
	g, _ := GroupOf(rd.State)
	data, err := json.Marshal(Observed{State: rd.State, Detail: rd.Detail, Group: g})
	if err == nil {
		_ = rf.WriteFile(runstore.FileState, append(data, '\n'))
	}
}

// ReadObserved reads state.json; ok is false when it is missing, unreadable
// or names a state the group table lacks.
func ReadObserved(rf *runstore.RunFolder) (Observed, bool) {
	data, _, err := rf.ReadFrom(runstore.FileState, 0)
	if err != nil {
		return Observed{}, false
	}
	var o Observed
	if json.Unmarshal(data, &o) != nil {
		return Observed{}, false
	}
	g, ok := GroupOf(o.State)
	o.Group = g
	return o, ok
}
