package indexer

import (
	"context"
	"os"
	"path/filepath"
)

// replayChunkBytes bounds how much NDJSON ReplaySession reads between budget
// checks. It is far below maxBytesPerTick because ReplaySession runs inside
// hooks with hard wall-clock caps (Codex kills SessionEnd at 3s), so the
// context must be checked often enough to stop on time.
const replayChunkBytes = 256 * 1024

// ReplayResult reports how far ReplaySession got through one session's log.
type ReplayResult struct {
	Offset   int64 // bytes of events.ndjson consumed
	Size     int64 // size of events.ndjson when the replay started
	Complete bool  // Offset reached Size before ctx expired
}

// ReplaySession replays one session's events.ndjson from byte 0 into the sink
// until end of file or until ctx expires. It is the per-session form of the
// indexer pass that `wipnote reindex` drives across every session.
//
// It starts at zero for the same reason the indexer does: the destination is a
// process-local projection that starts empty, so the on-disk .index-offset is
// never a resume point. It does NOT publish that marker either. The caller
// publishes it (PublishProgress) once it has extracted what it needs, because
// the marker authorises retention to archive the raw log.
//
// A spent budget is not an error: the result reports Complete=false and the
// caller leaves the marker alone so a later catch-up pass finishes the job.
func (idx *Indexer) ReplaySession(ctx context.Context, sessionID string) (ReplayResult, error) {
	path := filepath.Join(idx.wipnoteDir, "sessions", sessionID, "events.ndjson")
	info, err := os.Stat(path)
	if err != nil {
		return ReplayResult{}, err
	}
	res := ReplayResult{Size: info.Size()}
	for res.Offset < res.Size {
		if ctx.Err() != nil {
			return res, nil
		}
		next, err := idx.replayChunk(ctx, path, res.Offset, res.Size)
		if err != nil {
			if ctx.Err() != nil {
				return res, nil // the write was cut short by the budget
			}
			return res, err
		}
		if next <= res.Offset {
			break // defensive: no forward progress
		}
		res.Offset = next
	}
	// readLine also returns an unterminated final line, and readNewSignals
	// counts its missing newline, so a cut-off tail lands one byte past the
	// size. Clamp so the marker never claims bytes that do not exist.
	if res.Offset > res.Size {
		res.Offset = res.Size
	}
	res.Complete = res.Offset >= res.Size
	return res, nil
}

// replayChunk reads up to replayChunkBytes from offset, writes the parsed
// signals to the sink and returns the offset after the last consumed line.
func (idx *Indexer) replayChunk(ctx context.Context, path string, offset, size int64) (int64, error) {
	upTo := offset + replayChunkBytes
	if upTo > size {
		upTo = size
	}
	parsed, next, err := idx.readNewSignals(path, offset, upTo)
	if err != nil {
		return offset, err
	}
	if err := idx.writeParsedBatch(ctx, parsed); err != nil {
		return offset, err
	}
	return next, nil
}

// PublishProgress records at path that offset bytes of the session's NDJSON
// have been consumed. It is the exported form of publishProgress for callers
// that drive ReplaySession and decide for themselves when the log is safe to
// archive.
func PublishProgress(path string, offset int64) error {
	return publishProgress(path, offset)
}
