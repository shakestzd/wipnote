//go:build !windows

package claimledger

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

func shardInode(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := fi.Sys().(*syscall.Stat_t)
	if st == nil {
		return 0
	}
	return uint64(st.Ino)
}

// TestOpen_ContainerWorkerReplacesShardByRename: a container worker's claim
// shard is its own file, replaced by rename on every open (a new inode, both
// episodes kept); on the host an open appends in place (the inode stays).
func TestOpen_ContainerWorkerReplacesShardByRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inodes are a unix check")
	}
	for _, place := range []string{containerwrite.PlaceContainer, ""} {
		wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
		run := "dlg-0123456789ab"
		if err := os.MkdirAll(filepath.Join(wipnoteDir, "delegate", "runs", run), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(containerwrite.PlaceEnv, place)
		t.Setenv(containerwrite.RunEnv, run)
		s := NewStore(wipnoteDir)
		now := time.Now().UTC()
		mustOpen(t, s, "root-1", "root-1", "claude", "feat-a", now)
		first := shardInode(t, s.ShardPath("root-1"))
		mustOpen(t, s, "root-1", "root-1", "claude", "feat-b", now.Add(time.Second))
		renamed := shardInode(t, s.ShardPath("root-1")) != first
		if renamed != (place == containerwrite.PlaceContainer) {
			t.Errorf("place %q: shard replaced by rename = %v", place, renamed)
		}
		if eps, err := s.ReadShard("root-1"); err != nil || len(eps) != 2 {
			t.Errorf("place %q: %d episodes, %v; want 2", place, len(eps), err)
		}
	}
}
