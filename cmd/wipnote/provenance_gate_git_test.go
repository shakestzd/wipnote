package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// blobsInLooseBucket17 returns n file contents whose git blob ids start with
// "17". Git estimates the loose-object count from objects/17 alone, so two such
// blobs are enough to cross a 256-object auto-maintenance threshold.
func blobsInLooseBucket17(n int) []string {
	var out []string
	for i := 0; len(out) < n; i++ {
		c := fmt.Sprintf("payload %d\n", i)
		if h := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(c), c))); h[0] == 0x17 {
			out = append(out, c)
		}
	}
	return out
}

// forceEagerAutoMaintenance points git's global config at a file whose gc.auto
// fires on two loose objects in objects/17 — the same trigger git >= 2.54's
// default geometric-repack auto condition has — run in the foreground so the
// resulting pack is observable deterministically.
func forceEagerAutoMaintenance(t *testing.T) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[gc]\n\tauto = 1\n\tautoDetach = false\n[maintenance]\n\tautoDetach = false\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
}

// commitBucket17Blobs commits two blobs into objects/17 and reports how many
// packfiles the commit's auto maintenance left behind.
func commitBucket17Blobs(t *testing.T, root string) int {
	t.Helper()
	for i, c := range blobsInLooseBucket17(2) {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("b17-%d", i)), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	provGit(t, root, "add", "--all")
	provGit(t, root, "commit", "-q", "-m", "bucket 17")
	packs, _ := filepath.Glob(filepath.Join(root, ".git", "objects", "pack", "*.pack"))
	return len(packs)
}

// bug-a2ea88db: after each commit git runs `maintenance run --auto`, which
// detaches and can repack into .git/objects/pack after provGit has returned —
// racing t.TempDir's RemoveAll ("objects/pack: directory not empty"). The
// fixture repo must never start auto maintenance.
func TestProvInitRepo_DisablesAutoMaintenance(t *testing.T) {
	forceEagerAutoMaintenance(t)

	// Control: an ordinary repo under the same config does repack, so the
	// fixture's zero below is not vacuous.
	plain := t.TempDir()
	provGit(t, plain, "init", "-q")
	if n := commitBucket17Blobs(t, plain); n == 0 {
		t.Fatal("control repo ran no auto maintenance; the forcing config is not in effect")
	}

	fixture := t.TempDir()
	provInitRepo(t, fixture)
	if n := commitBucket17Blobs(t, fixture); n != 0 {
		t.Errorf("provInitRepo repo ran auto maintenance after a commit: %d pack(s) written", n)
	}
}
