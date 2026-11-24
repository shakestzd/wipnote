// Package scratch saves a session's authored scratch drafts into the wipnote
// store, so a restart or the harness's retention sweep cannot lose them.
//
// Capture walks the harness scratchpad folder once, within fixed limits. It
// copies changed authored documents to <store>/scratch/<session>/files/ and
// lists every scratch file in <store>/scratch/<session>/manifest.json. Every
// writer of the manifest goes through Update, which holds one file lock, so
// no writer drops another's rows.
//
// The package imports nothing from core/hooks: the hook passes the session id
// and the active work item in.
package scratch

import "time"

// Fixed limits of one capture pass (owner decision, plan-13a9e0c8).
const (
	// MaxFileBytes is the largest draft Capture copies.
	MaxFileBytes = 256 << 10
	// MaxSessionBytes is the most saved bytes one session may hold.
	MaxSessionBytes = 5 << 20
	// MaxWalkFiles is the most scratchpad entries one pass visits.
	MaxWalkFiles = 2000
	// WalkBudget is the longest one pass walks before it stops early.
	WalkBudget = 2 * time.Second
)

// Kinds of scratch file, from the file name.
const (
	KindDocument = "document"
	KindScript   = "script"
	KindData     = "data"
	KindLog      = "log"
	KindImage    = "image"
	KindTree     = "tree"
	KindOther    = "other"
)

// Reasons a scratch file was listed and not copied.
const (
	SkipNotAuthored = "not-authored"
	SkipOversize    = "oversize"
	SkipSessionCap  = "session-cap"
	SkipBinary      = "binary"
	SkipSecret      = "secret"
	SkipTree        = "tree"
	SkipSymlink     = "symlink"
	SkipBudget      = "budget"
)

// States of a manifest row.
const (
	StatePresent = "present" // seen in the scratchpad by the last full pass
	StateGone    = "gone"    // no longer in the scratchpad; a saved copy stays
	StateExpired = "expired" // retention dropped it; the same content is never saved again
)

// Values of Manifest.StoppedEarly.
const (
	StopTimeBudget = "time-budget"
	StopFileCount  = "file-count"
)

// limits are the bounds of one pass. Production uses defaultLimits; tests
// shrink them.
type limits struct {
	maxFile    int64
	maxSession int64
	maxFiles   int
	budget     time.Duration
	now        func() time.Time
}

func defaultLimits() limits {
	return limits{
		maxFile:    MaxFileBytes,
		maxSession: MaxSessionBytes,
		maxFiles:   MaxWalkFiles,
		budget:     WalkBudget,
		now:        time.Now,
	}
}
