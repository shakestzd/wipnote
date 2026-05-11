package indexer

import (
	"bufio"
	"context"
	"database/sql"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/otel"
	"github.com/shakestzd/wipnote/internal/otel/sink"
)

// syncSink is the optional interface implemented by sinks that expose a
// synchronous write path (currently sqlite.QueuedSink). When the
// configured sink satisfies it, the indexer routes batches through the
// sync path so it can wait for the SQLite commit before advancing
// `.index-offset`. Without the sync path, the underlying queue is
// fire-and-forget and a queue rejection / late op error would silently
// strand records as "indexed" in the checkpoint while the SQLite insert
// never happened (roborev #1501).
type syncSink interface {
	WriteBatchSync(ctx context.Context, harness otel.Harness, resourceAttrs map[string]any, signals []otel.UnifiedSignal) error
}

const pollInterval = 500 * time.Millisecond

// maxBytesPerTick caps the amount of NDJSON data processed per session per tick.
// Prevents a single huge session from monopolizing the SQLite writer lock and
// starving other sessions, which manifested as bug-faf8e395 (indexer retry loop
// on a 366MB events.ndjson).
const maxBytesPerTick = 4 * 1024 * 1024 // 4 MiB

// FileInfo holds per-file health metrics for the /api/indexer/status endpoint.
type FileInfo struct {
	LastOffset    int64     `json:"last_offset"`
	CurrentSize   int64     `json:"current_size"`
	LagBytes      int64     `json:"lag_bytes"`
	LastError     string    `json:"last_error"`
	LastIndexedAt time.Time `json:"last_indexed_at"`
}

// Indexer polls .wipnote/sessions/*/events.ndjson files for new appends,
// parses each line into a UnifiedSignal, and applies them to SQLite via snk.
type Indexer struct {
	wipnoteDir string
	snk        sink.SignalSink
	database   *sql.DB // optional; enables prompt_id bridging when set

	mu     sync.RWMutex
	status map[string]FileInfo
}

// New constructs an Indexer rooted at wipnoteDir.
// wipnoteDir is the .wipnote/ directory (e.g. /path/to/project/.wipnote).
func New(wipnoteDir string, snk sink.SignalSink) *Indexer {
	return &Indexer{
		wipnoteDir: wipnoteDir,
		snk:        snk,
		status:     make(map[string]FileInfo),
	}
}

// WithDB attaches a *sql.DB to the indexer so it can bridge OTel prompt_id
// values back to the corresponding UserQuery rows in agent_events. This is
// optional: when not set, prompt_id bridging is silently skipped.
func (idx *Indexer) WithDB(database *sql.DB) *Indexer {
	idx.database = database
	return idx
}

// Start runs the poll loop until ctx is cancelled. Intended to be called as a goroutine.
func (idx *Indexer) Start(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			idx.runOnce(ctx)
		}
	}
}

// Status returns a snapshot of per-session file health.
func (idx *Indexer) Status() map[string]FileInfo {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make(map[string]FileInfo, len(idx.status))
	for k, v := range idx.status {
		out[k] = v
	}
	return out
}

// RunOnce performs one indexer pass synchronously. It exists so callers
// outside the daemon (notably `wipnote reindex` — slice 9, feat-229f3333)
// can drive the same NDJSON-to-SQLite pipeline used by Start, but in
// foreground mode for full-rebuild scenarios. Idempotent: replaying the
// same offsets is safe because the receiver Writer uses INSERT OR IGNORE.
func (idx *Indexer) RunOnce(ctx context.Context) {
	idx.runOnce(ctx)
}

// runOnce discovers all sessions and processes any new data.
func (idx *Indexer) runOnce(ctx context.Context) {
	sessions, err := idx.discoverSessions()
	if err != nil {
		log.Printf("indexer: discover sessions: %v", err)
		return
	}
	for _, sid := range sessions {
		if ctx.Err() != nil {
			return
		}
		if err := idx.processSession(ctx, sid); err != nil {
			log.Printf("indexer: session %s: %v", sid, err)
			idx.recordError(sid, err)
		}
	}
}

// discoverSessions returns session IDs that have an events.ndjson file.
// When the indexer has a database attached (idx.database != nil), it also
// filters out session directories that have no corresponding row in the
// sessions table (orphans). Orphan directories are logged at debug level and
// skipped so the indexer never wastes writer cycles on data that cannot be
// attributed to a known session.
func (idx *Indexer) discoverSessions() ([]string, error) {
	sessionsDir := filepath.Join(idx.wipnoteDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var sessions []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ndjson := filepath.Join(sessionsDir, e.Name(), "events.ndjson")
		if _, err := os.Stat(ndjson); err == nil {
			sessions = append(sessions, e.Name())
		}
	}

	// Gate on DB membership: skip orphan directories (no sessions row).
	sessions = filterSessionsByDB(idx.database, idx.wipnoteDir, sessions)
	return sessions, nil
}

// processSession tails events.ndjson for sessionID from the last checkpoint,
// parses each line, and applies the batch to snk. On success, writes a new checkpoint.
func (idx *Indexer) processSession(ctx context.Context, sessionID string) error {
	sessDir := filepath.Join(idx.wipnoteDir, "sessions", sessionID)
	ndjsonPath := filepath.Join(sessDir, "events.ndjson")
	checkpointPath := filepath.Join(sessDir, ".index-offset")

	offset, err := readCheckpoint(checkpointPath)
	if err != nil {
		return err
	}

	info, err := os.Stat(ndjsonPath)
	if err != nil {
		return err
	}
	currentSize := info.Size()
	idx.updateSize(sessionID, offset, currentSize)

	if currentSize <= offset {
		return nil // no new data
	}

	// Cap the read window to maxBytesPerTick so a single huge file cannot
	// monopolize the SQLite writer lock across ticks (bug-faf8e395).
	readUpTo := currentSize
	if readUpTo-offset > maxBytesPerTick {
		readUpTo = offset + maxBytesPerTick
	}

	parsed, newOffset, err := idx.readNewSignals(ndjsonPath, offset, readUpTo)
	if err != nil {
		return err
	}
	if len(parsed) == 0 {
		return writeCheckpoint(checkpointPath, newOffset)
	}

	if err := idx.writeParsedBatch(ctx, parsed); err != nil {
		return err
	}

	if err := writeCheckpoint(checkpointPath, newOffset); err != nil {
		return err
	}

	idx.recordSuccess(sessionID, newOffset, currentSize)
	return nil
}

// readNewSignals opens ndjsonPath, seeks to offset, reads complete
// newline-terminated lines up to readUpTo bytes, and parses them.
// Incomplete trailing data (no newline at EOF) is left uncheckpointed
// so the next poll retries once the writer finishes the line.
// readUpTo limits how many bytes are consumed per tick (bug-faf8e395).
func (idx *Indexer) readNewSignals(ndjsonPath string, offset, readUpTo int64) ([]parsedSignal, int64, error) {
	f, err := os.Open(ndjsonPath)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, offset, err
		}
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	var result []parsedSignal
	committedOffset := offset

	for {
		// Stop once we've reached or passed the byte budget ceiling.
		// Because readLine consumes exactly one complete newline-terminated
		// record, the cutoff is always on a newline boundary.
		if committedOffset >= readUpTo {
			break
		}
		line, err := readLine(reader)
		if err != nil {
			break
		}
		lineLen := int64(len(line)) + 1
		if len(line) == 0 {
			committedOffset += lineLen
			continue
		}
		p, parseErr := parseLine(line)
		if parseErr != nil {
			log.Printf("indexer: skip malformed line at offset ~%d: %v",
				committedOffset, parseErr)
			committedOffset += lineLen
			continue
		}
		if p == nil {
			committedOffset += lineLen
			continue
		}
		result = append(result, *p)
		committedOffset += lineLen
	}
	return result, committedOffset, nil
}

// writeParsedBatch writes parsed signals to the sink, passing through
// each signal's resource attributes so placeholder/re-attribution logic
// in the SQLite writer functions correctly. After persisting each signal it
// attempts to bridge prompt_id from user_prompt log records back to the
// matching UserQuery row in agent_events (best-effort, silently skipped on failure).
//
// When the sink supports a synchronous path (sqlite.QueuedSink does), we
// use it so the caller can refuse to advance the `.index-offset`
// checkpoint until the SQLite commit succeeds. This closes roborev
// #1501: WriteBatch on the QueuedSink is fire-and-forget, so without
// the sync variant a queue rejection (full / unavailable / timeout) or
// a late op error would let processSession checkpoint records as
// "indexed" while the DB write never happened.
func (idx *Indexer) writeParsedBatch(ctx context.Context, parsed []parsedSignal) error {
	sync, useSync := idx.snk.(syncSink)
	for _, p := range parsed {
		h := p.Signal.Harness
		if h == "" {
			h = otel.HarnessClaude
		}
		signals := []otel.UnifiedSignal{p.Signal}
		var err error
		if useSync {
			err = sync.WriteBatchSync(ctx, h, p.ResourceAttrs, signals)
		} else {
			err = idx.snk.WriteBatch(ctx, h, p.ResourceAttrs, signals)
		}
		if err != nil {
			return err
		}
		idx.maybeSetPromptID(p.Signal)
	}
	return nil
}

// maybeSetPromptID correlates a user_prompt OTel signal back to the closest
// UserQuery event in agent_events by session_id + timestamp. It is a no-op
// when the indexer has no database attached, the signal is not a user_prompt,
// or the signal carries no prompt_id.
func (idx *Indexer) maybeSetPromptID(sig otel.UnifiedSignal) {
	if idx.database == nil {
		return
	}
	if sig.Kind != otel.KindLog {
		return
	}
	if sig.CanonicalName != otel.CanonicalUserPrompt {
		return
	}
	if sig.PromptID == "" || sig.SessionID == "" {
		return
	}
	if err := db.SetPromptID(idx.database, sig.SessionID, sig.PromptID, sig.Timestamp); err != nil {
		log.Printf("indexer: set prompt_id (session=%s, prompt=%s): %v",
			sig.SessionID, sig.PromptID, err)
	}
}

const maxLineSize = 4 * 1024 * 1024

// readLine reads until the next newline, returning the line content
// without the newline. Returns io.EOF when no more complete lines
// exist. Lines exceeding maxLineSize are skipped with a log warning.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if !isPrefix {
			return buf, nil
		}
		if len(buf) > maxLineSize {
			skipToNewline(r)
			log.Printf("indexer: line exceeds %d bytes — skipped", maxLineSize)
			return buf[:0], nil // return empty so caller advances offset
		}
	}
}

func skipToNewline(r *bufio.Reader) {
	for {
		_, isPrefix, err := r.ReadLine()
		if err != nil || !isPrefix {
			return
		}
	}
}

// updateSize records the current file size without touching LastIndexedAt.
func (idx *Indexer) updateSize(sessionID string, offset, currentSize int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	fi := idx.status[sessionID]
	fi.LastOffset = offset
	fi.CurrentSize = currentSize
	fi.LagBytes = currentSize - offset
	idx.status[sessionID] = fi
}

// recordSuccess updates the status snapshot after a successful batch.
func (idx *Indexer) recordSuccess(sessionID string, newOffset, currentSize int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.status[sessionID] = FileInfo{
		LastOffset:    newOffset,
		CurrentSize:   currentSize,
		LagBytes:      currentSize - newOffset,
		LastIndexedAt: time.Now().UTC(),
	}
}

// recordError updates the last_error field in the status snapshot.
func (idx *Indexer) recordError(sessionID string, err error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	fi := idx.status[sessionID]
	fi.LastError = err.Error()
	idx.status[sessionID] = fi
}
