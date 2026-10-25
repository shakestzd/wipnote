package db_test

import "sync"

// migrationCallRecorder records every migration the observer reports.
type migrationCallRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *migrationCallRecorder) Record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *migrationCallRecorder) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}
