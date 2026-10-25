package blame

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/db/dbtest"
	"github.com/shakestzd/wipnote/core/models"
)

// countQueries wraps the queryRows seam and returns a pointer to the count.
func countQueries(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := queryRows
	queryRows = func(ctx context.Context, d *sql.DB, q string, args ...any) (*sql.Rows, error) {
		n++
		return orig(ctx, d, q, args...)
	}
	t.Cleanup(func() { queryRows = orig })
	return &n
}

// indexFixture is a git repo plus feature_files rows that exercise the old
// per-file match: exact relative paths, absolute worktree-prefixed paths,
// LIKE's ASCII case folding, a literal '_' and near-miss suffixes.
func indexFixture(t *testing.T) (*sql.DB, string, []string) {
	t.Helper()
	database, err := dbtest.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	mustExec(t, database, `INSERT INTO tracks (id, type, title, status, priority) VALUES ('trk-a','track','Track A','active','medium')`)
	for _, f := range [][2]string{{"feat-1", "trk-a"}, {"feat-2", "trk-a"}, {"feat-3", ""}} {
		track := sql.NullString{String: f[1], Valid: f[1] != ""}
		mustExec(t, database, `INSERT INTO features (id, type, title, status, priority, track_id) VALUES (?, 'feature', ?, 'active', 'medium', ?)`, f[0], "Title "+f[0], track)
	}
	files := []string{"pkg/file.go", "pkg/a_b.go", "README.md", "readme.md", "cmd/main.go", "solo.go"}
	rows := [][2]string{
		{"feat-1", "pkg/file.go"}, {"feat-1", "/wt/one/pkg/file.go"}, {"feat-2", "/wt/two/PKG/File.go"},
		{"feat-2", "/wt/pkg/aXb.go"}, {"feat-3", "/wt/pkg/a_b.go"}, {"feat-1", "/x/README.md"},
		{"feat-3", "xcmd/main.go"}, {"feat-3", "/abs/cmd/main.go"}, {"feat-2", "solo.go"},
		{"feat-ghost", "/gone/solo.go"},
	}
	for i, r := range rows {
		if err := db.UpsertFeatureFile(database, &models.FeatureFile{
			ID: "ff-" + string(rune('a'+i)), FeatureID: r[0], FilePath: r[1], Operation: "edit",
		}); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	for _, f := range files {
		abs := filepath.Join(root, f)
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		if err := os.WriteFile(abs, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCommitAll(t, root)
	return database, root, files
}

func mustExec(t *testing.T, d *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func gitCommitAll(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "seed"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// TestWalkAreas_SingleQuery pins feat-c30d0792: WalkAreas issues one
// database query for the whole tree, not one LIKE scan per tracked file.
func TestWalkAreas_SingleQuery(t *testing.T) {
	database, root, _ := indexFixture(t)
	n := countQueries(t)
	if _, err := WalkAreas(context.Background(), database, root, WalkOptions{}); err != nil {
		t.Fatal(err)
	}
	if *n != 1 {
		t.Errorf("WalkAreas issued %d queries, want 1", *n)
	}
}

// TestFileIndex_MatchesPerFileQuery pins that the one-scan index gives every
// tracked file exactly the Result the per-file LIKE query gave, so WalkAreas
// output is unchanged.
func TestFileIndex_MatchesPerFileQuery(t *testing.T) {
	database, _, files := indexFixture(t)
	ctx := context.Background()
	idx, err := loadFileIndex(ctx, database, files)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		want, err := Query(ctx, database, rel, QueryOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got := idx.result(rel)
		sortByID(want.Features)
		sortByID(got.Features)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got  %+v\n want %+v", rel, got, want)
		}
	}
	// Not vacuous: the fixture's matches and near-misses land where the LIKE
	// semantics put them.
	touches := func(rel, feat string) int {
		for _, f := range idx.result(rel).Features {
			if f.ID == feat {
				return f.TouchCount
			}
		}
		return 0
	}
	for _, c := range []struct {
		rel, feat string
		want      int
	}{
		{"pkg/file.go", "feat-1", 2}, {"pkg/file.go", "feat-2", 1}, // ASCII case fold
		{"pkg/a_b.go", "feat-3", 1}, {"pkg/a_b.go", "feat-2", 0}, // '_' is literal
		{"readme.md", "feat-1", 1}, {"README.md", "feat-1", 1},
		{"cmd/main.go", "feat-3", 1}, // "xcmd/main.go" has no '/' boundary
		{"solo.go", "feat-ghost", 1},
	} {
		if got := touches(c.rel, c.feat); got != c.want {
			t.Errorf("%s/%s touches = %d, want %d", c.rel, c.feat, got, c.want)
		}
	}
}

func sortByID(rows []FeatureRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
}
