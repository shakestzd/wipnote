package claimledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/filelock"
)

// Container workers (plan-28b4a7f6 slice 21). Across the bind mount flock
// does not exclude the host and a rename from one side can drop the other
// side's write, so a container worker never writes a shared claim shard.
// Its own shards live in its run folder (<run>/claims/), replaced by rename;
// its reads see them over the shared ones. A completion that would close a
// shared shard's episode leaves a PendingClose there instead. The host fold
// merges the own shards (MergeOwnShard) and applies the pending closes under
// each shard's lock.

// ErrContainerWorker refuses a claim-ledger pass that rewrites shared shards
// (reconcile, archive, the host merge) inside a container worker.
var ErrContainerWorker = errors.New("claimledger: a container worker does not write the shared claim shards")

// pendingPrefix names a worker's pending closes in its run folder.
const pendingPrefix = "claim-close-"

// PendingClose is a completion's close of work-item episodes in shards the
// worker does not own, for the host to apply (CloseWorkItem's arguments).
type PendingClose struct {
	ID         string    `json:"id"`
	WorkItemID string    `json:"work_item"`
	Outcome    Outcome   `json:"outcome"`
	EndedAt    time.Time `json:"ended_at"`
	StartedBy  time.Time `json:"started_by,omitzero"`
}

// ownDir is a container worker's own claims folder; ok is false elsewhere.
func ownDir(wipnoteDir string) (string, bool) {
	if !containerwrite.Active() {
		return "", false
	}
	run, err := containerwrite.RunDir(wipnoteDir)
	if err != nil {
		return "", false
	}
	return filepath.Join(run, DirName), true
}

// shardDir is where this process writes a root session's shard.
func (s *Store) shardDir() string {
	if d, ok := ownDir(s.wipnoteDir); ok {
		return d
	}
	return s.dir
}

// closeWorkItemInside is CloseWorkItem in a container worker: its own shards
// are closed in place; an open episode on the item in a shared shard is left
// as a PendingClose for the host.
func (s *Store) closeWorkItemInside(paths []string, workItemID string, outcome Outcome, endedAt, startedBy time.Time) (int, error) {
	own := s.shardDir()
	total, foreign := 0, false
	for _, path := range paths {
		if filepath.Dir(path) == own {
			n, err := closeWorkItemInShard(s, path, workItemID, outcome, endedAt, startedBy)
			total += n
			if err != nil {
				return total, err
			}
			continue
		}
		eps, _ := readShard(path)
		for _, e := range eps {
			if e.IsOpen() && e.WorkItemID == workItemID && (startedBy.IsZero() || !e.StartedAt.After(startedBy)) {
				foreign = true
			}
		}
	}
	if !foreign {
		return total, nil
	}
	op := PendingClose{ID: NewEpisodeID(), WorkItemID: workItemID, Outcome: outcome, EndedAt: endedAt.UTC(), StartedBy: startedBy.UTC()}
	return total, containerwrite.WriteJSON(s.wipnoteDir, pendingPrefix+op.ID+".json", op)
}

// ReadPendingCloses reads the pending closes in a worker's run folder.
func ReadPendingCloses(runDir string) ([]PendingClose, error) {
	var out []PendingClose
	err := containerwrite.EachFile(runDir, pendingPrefix, func(_ string, data []byte) {
		var op PendingClose
		if json.Unmarshal(data, &op) == nil && op.ID != "" && op.WorkItemID != "" && op.Outcome.valid() {
			out = append(out, op)
		}
	})
	return out, err
}

// OwnShards returns a worker's own shard files in its run folder.
func OwnShards(runDir string) ([]string, error) { return shardsIn(filepath.Join(runDir, DirName)) }

// MergeOwnShard folds a container worker's own shard (named as it is, such
// as sess-<root>.html) into the shared shard of that name, under its lock:
// an episode the shared shard lacks is added, and an end the worker recorded
// on an episode still open there is set. Episodes the host wrote are kept.
// It reports how many episodes changed. Only the host merges.
func (s *Store) MergeOwnShard(name string, own []Episode) (int, error) {
	if containerwrite.Active() {
		return 0, ErrContainerWorker
	}
	if name != filepath.Base(name) || !strings.HasPrefix(name, shardPrefix) || !strings.HasSuffix(name, ".html") {
		return 0, fmt.Errorf("claimledger: %q is not a shard name", name)
	}
	path := filepath.Join(s.dir, name)
	release := filelock.Guard(path)
	defer release()
	have, err := readShard(path)
	if err != nil {
		return 0, err
	}
	merged, changed := mergeEpisodes(have, own)
	if changed == 0 {
		return 0, nil
	}
	if err := writeAllLocked(path, merged); err != nil {
		return 0, err
	}
	s.notify(path, "claim")
	return changed, nil
}

func mergeEpisodes(have, own []Episode) ([]Episode, int) {
	at := map[string]int{}
	for i, e := range have {
		at[e.ID] = i
	}
	changed := 0
	for _, e := range own {
		i, ok := at[e.ID]
		switch {
		case !ok:
			have = append(have, e)
			at[e.ID] = len(have) - 1
			changed++
		case have[i].IsOpen() && !e.IsOpen():
			have[i].EndedAt, have[i].Outcome = e.EndedAt, e.Outcome
			changed++
		}
	}
	return have, changed
}
