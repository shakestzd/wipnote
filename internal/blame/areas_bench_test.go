package blame_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/internal/blame"
)

// areasFixture builds a git repo of nFiles files and a feature_files table in
// which each file is touched by perFile features, stored the way the hooks
// record them (absolute, worktree-prefixed paths) plus one exact relative row.
func areasFixture(tb testing.TB, nFiles, perFile int) (*sql.DB, string) {
	tb.Helper()
	database := openTestDB(tb)
	root := tb.TempDir()
	for tr := 0; tr < 4; tr++ {
		insertTrack(tb, database, fmt.Sprintf("trk-bench-%d", tr), fmt.Sprintf("Track %d", tr))
	}
	for f := 0; f < 20; f++ {
		insertFeature(tb, database, fmt.Sprintf("feat-bench-%02d", f), fmt.Sprintf("Feature %d", f), fmt.Sprintf("trk-bench-%d", f%4))
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		tb.Fatal(err)
	}
	files := make([]string, 0, nFiles)
	for i := 0; i < nFiles; i++ {
		rel := fmt.Sprintf("pkg/file_%04d.go", i)
		writeFile(tb, root, rel)
		files = append(files, rel)
		for k := 0; k < perFile; k++ {
			feat := fmt.Sprintf("feat-bench-%02d", (i+k)%20)
			path := "/home/dev/wt-" + fmt.Sprint(k) + "/" + rel
			if k == 0 {
				path = rel
			}
			insertFeatureFile(tb, database, fmt.Sprintf("ff-%d-%d", i, k), feat, path)
		}
	}
	gitInit(tb, root, files...)
	return database, root
}

func BenchmarkWalkAreas_2000Files(b *testing.B) {
	database, root := areasFixture(b, 2000, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := blame.WalkAreas(context.Background(), database, root, blame.WalkOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
