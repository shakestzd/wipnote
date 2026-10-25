package filelock

import (
	"fmt"
	"os"
	"sync/atomic"
)

// atomicWriteSeq makes each temp name unique within one process, so two
// goroutines writing the same path never share a temp file.
var atomicWriteSeq atomic.Int64

// WriteFileAtomic replaces path with data so readers see either the old or
// the new content, never a partial file. It writes "<path>.tmp.<pid>.<seq>"
// in the same directory with perm, fsyncs it, and renames it over path; the
// temp file is removed on any failure. It takes no lock: callers that need
// writers serialised hold a Guard. The parent directory must exist.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), atomicWriteSeq.Add(1))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp to target: %w", err)
	}
	return nil
}
