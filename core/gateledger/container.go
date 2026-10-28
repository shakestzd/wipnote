package gateledger

import (
	"encoding/json"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

// ownPrefix names a container worker's own gate files, gate-<id>.json in its
// run folder. The host folds them into the shared ledger (plan-28b4a7f6
// slice 21); the worker never writes the ledger across the mount.
const ownPrefix = "gate-"

func appendOwn(wipnoteDir string, r Record) error {
	return containerwrite.WriteJSON(wipnoteDir, ownPrefix+r.ID+".json", r)
}

// withOwn adds a container worker's own records to recs, so its completion
// gate sees the runs it recorded before the host folded them. Outside a
// container worker recs is returned as it is.
func withOwn(wipnoteDir string, recs []Record) []Record {
	dir, err := containerwrite.RunDir(wipnoteDir)
	if err != nil {
		return recs
	}
	own, _ := ReadOwn(dir)
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.ID] = true
	}
	for _, r := range own {
		if !seen[r.ID] {
			recs = append(recs, r)
		}
	}
	sortRecords(recs)
	return recs
}

// ReadOwn reads the own gate files in a run folder. A file that does not
// decode or validate is skipped.
func ReadOwn(runDir string) ([]Record, error) {
	var out []Record
	err := containerwrite.EachFile(runDir, ownPrefix, func(_ string, data []byte) {
		var r Record
		if json.Unmarshal(data, &r) == nil && r.Validate() == nil {
			out = append(out, r)
		}
	})
	return out, err
}
