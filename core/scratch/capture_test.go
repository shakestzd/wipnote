package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Done-when 1, 8 and 9: a new top-level draft is copied with mode 0600 and
// listed as saved, and the manifest records the scratchpad folder.
func TestCapture_SavesNewTopLevelDraft(t *testing.T) {
	e := newEnv(t)
	e.write("draft.md", "# Draft\n\nbody\n")

	m, res := e.capture("feat-aaaa0001")

	r := row(t, m, "draft.md")
	if !r.Saved || r.SkipReason != "" || r.Kind != KindDocument || r.State != StatePresent || r.Pinned {
		t.Errorf("row = %+v, want a saved present document, not pinned", r)
	}
	if r.Size != int64(len("# Draft\n\nbody\n")) || len(r.SHA256) != 64 || r.MTime == "" {
		t.Errorf("row = %+v, want size, a sha256 and an mtime", r)
	}
	if !reflect.DeepEqual(r.WorkItems, []string{"feat-aaaa0001"}) {
		t.Errorf("work_items = %v, want [feat-aaaa0001]", r.WorkItems)
	}
	got, err := os.ReadFile(e.copyPath("draft.md"))
	if err != nil || string(got) != "# Draft\n\nbody\n" {
		t.Errorf("saved copy = %q, %v", got, err)
	}
	for _, p := range []string{e.copyPath("draft.md"), ManifestPath(e.sessionDir())} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, err %v, want 0600", p, fi.Mode().Perm(), err)
		}
	}
	if m.ScratchpadDir != e.pad || m.SessionID != testSession || m.StoppedEarly != "" {
		t.Errorf("manifest = %+v, want scratchpad_dir %q and session %q", m, e.pad, testSession)
	}
	if res.Saved != 1 || !res.Wrote {
		t.Errorf("result = %+v, want one save and a write", res)
	}
}

// A draft with no active work item has an empty work_items list.
func TestCapture_NoActiveItemListsNone(t *testing.T) {
	e := newEnv(t)
	e.write("notes/plan.txt", "plan\n")
	m, _ := e.capture("")
	if r := row(t, m, "notes/plan.txt"); !r.Saved || len(r.WorkItems) != 0 {
		t.Errorf("row = %+v, want saved with no work items", r)
	}
	raw, _ := os.ReadFile(ManifestPath(e.sessionDir()))
	if !strings.Contains(string(raw), `"work_items": []`) {
		t.Errorf("manifest must write an empty list, got:\n%s", raw)
	}
}

// Done-when 2: a second pass with no scratch change writes nothing.
func TestCapture_SecondPassWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.write("draft.md", "one\n")
	e.write("sub/other.rst", "two\n")
	e.write("run.log", "noise\n")
	e.capture("feat-aaaa0001")
	before := snapshot(t, filepath.Join(e.store, "scratch"))

	_, res := e.capture("feat-aaaa0001")

	if after := snapshot(t, filepath.Join(e.store, "scratch")); !reflect.DeepEqual(before, after) {
		t.Errorf("second pass changed the store:\nbefore %v\nafter  %v", before, after)
	}
	if res.Wrote || res.Saved != 0 {
		t.Errorf("result = %+v, want no write and no save", res)
	}
}

// Done-when 3: a draft that changes under a new active item gains that item
// and keeps the old one. An unchanged draft keeps its list.
func TestCapture_ChangedDraftGainsNewWorkItem(t *testing.T) {
	e := newEnv(t)
	e.write("changed.md", "v1\n")
	e.write("same.md", "still\n")
	e.capture("feat-aaaa0001")

	e.write("changed.md", "v2 longer\n")
	m, _ := e.capture("feat-bbbb0002")

	if got := row(t, m, "changed.md").WorkItems; !reflect.DeepEqual(got, []string{"feat-aaaa0001", "feat-bbbb0002"}) {
		t.Errorf("changed draft work_items = %v, want both items", got)
	}
	if got := row(t, m, "same.md").WorkItems; !reflect.DeepEqual(got, []string{"feat-aaaa0001"}) {
		t.Errorf("unchanged draft work_items = %v, want only the first item", got)
	}
	if got, _ := os.ReadFile(e.copyPath("changed.md")); string(got) != "v2 longer\n" {
		t.Errorf("saved copy = %q, want the new content", got)
	}
}

// Done-when 4: a deleted draft keeps its copy and its row is marked gone.
func TestCapture_DeletedDraftKeepsCopyMarkedGone(t *testing.T) {
	e := newEnv(t)
	path := e.write("draft.md", "keep me\n")
	e.write("stays.md", "here\n")
	e.capture("")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	m, _ := e.capture("")

	if r := row(t, m, "draft.md"); r.State != StateGone || !r.Saved {
		t.Errorf("row = %+v, want gone and still saved", r)
	}
	if got, _ := os.ReadFile(e.copyPath("draft.md")); string(got) != "keep me\n" {
		t.Errorf("saved copy = %q, want it kept", got)
	}
	if r := row(t, m, "stays.md"); r.State != StatePresent {
		t.Errorf("row = %+v, want present", r)
	}
}

// Done-when 5: a renamed draft gets a new row and the old row is marked gone.
func TestCapture_RenamedDraftGetsNewRow(t *testing.T) {
	e := newEnv(t)
	path := e.write("old.md", "text\n")
	e.capture("")
	if err := os.Rename(path, filepath.Join(e.pad, "new.md")); err != nil {
		t.Fatal(err)
	}

	m, _ := e.capture("")

	if r := row(t, m, "old.md"); r.State != StateGone || !r.Saved {
		t.Errorf("old row = %+v, want gone and saved", r)
	}
	if r := row(t, m, "new.md"); r.State != StatePresent || !r.Saved {
		t.Errorf("new row = %+v, want present and saved", r)
	}
	if !e.hasCopy("old.md") || !e.hasCopy("new.md") {
		t.Error("want a saved copy under both names")
	}
}

// A draft that comes back after it was marked gone is present again.
func TestCapture_ReturnedDraftIsPresentAgain(t *testing.T) {
	e := newEnv(t)
	path := e.write("draft.md", "text\n")
	e.capture("")
	_ = os.Remove(path)
	e.capture("")
	e.write("draft.md", "text\n")

	m, res := e.capture("")

	if r := row(t, m, "draft.md"); r.State != StatePresent || !r.Saved {
		t.Errorf("row = %+v, want present and saved", r)
	}
	if res.Saved != 0 {
		t.Errorf("same content was copied again: %+v", res)
	}
}

// Done-when 6: content whose hash matches an expired row is not saved again,
// under the same path or a new one.
func TestCapture_ExpiredHashNotSavedAgain(t *testing.T) {
	e := newEnv(t)
	e.write("draft.md", "old news\n")
	e.capture("")
	err := Update(e.store, testSession, func(m *Manifest) error {
		for i := range m.Files {
			m.Files[i].State = StateExpired
		}
		return os.Remove(e.copyPath("draft.md"))
	})
	if err != nil {
		t.Fatal(err)
	}
	e.touch("draft.md")
	e.write("copy.md", "old news\n")

	m, res := e.capture("")

	if res.Saved != 0 || e.hasCopy("draft.md") || e.hasCopy("copy.md") {
		t.Errorf("expired content was saved again: %+v", res)
	}
	if r := row(t, m, "draft.md"); r.State != StateExpired {
		t.Errorf("row = %+v, want it still expired", r)
	}
	if r := row(t, m, "copy.md"); r.State != StateExpired || r.Saved {
		t.Errorf("row = %+v, want expired and not saved", r)
	}
	if _, again := e.capture(""); again.Wrote {
		t.Error("a pass over expired content wrote the manifest again")
	}
}

// Done-when 7: once saved files reach the session cap, a new file is listed
// with skip reason session-cap.
func TestCapture_SessionCap(t *testing.T) {
	e := newEnv(t)
	e.lim.maxSession = 10
	e.write("a.md", "123456")
	if m, _ := e.capture(""); !row(t, m, "a.md").Saved {
		t.Fatal("first draft must fit under the cap")
	}
	e.write("b.md", "123456")

	m, _ := e.capture("")

	e.wantSkipped(m, "b.md", KindDocument, SkipSessionCap)
	if !row(t, m, "a.md").Saved {
		t.Error("the first draft must stay saved")
	}
}

// Done-when 10: a subfolder that holds a project marker is one tree row and
// nothing inside it is copied.
func TestCapture_ProjectTreeIsOneRow(t *testing.T) {
	for _, marker := range treeMarkers {
		t.Run(marker, func(t *testing.T) {
			e := newEnv(t)
			if marker == ".git" || marker == "node_modules" {
				e.write("clone/"+marker+"/inner.md", "x\n")
			} else {
				e.write("clone/"+marker, "x\n")
			}
			e.write("clone/README.md", "# readme\n")
			e.write("clone/docs/deep.md", "deep\n")
			e.write("draft.md", "mine\n")

			m, _ := e.capture("")

			e.wantSkipped(m, "clone", KindTree, SkipTree)
			if len(m.Files) != 2 || !row(t, m, "draft.md").Saved {
				t.Errorf("rows = %+v, want one tree row and the saved draft", m.Files)
			}
			if _, err := os.Lstat(e.copyPath("clone")); err == nil {
				t.Error("files from the tree were copied")
			}
		})
	}
}

// Done-when 11: a marker file at the top of the scratchpad does not make the
// whole scratchpad a tree.
func TestCapture_TopLevelMarkerIsNotATree(t *testing.T) {
	e := newEnv(t)
	e.write("go.mod", "module x\n")
	e.write("package.json", "{}\n")
	e.write("draft.md", "mine\n")
	e.write("notes/more.md", "more\n")

	m, _ := e.capture("")

	if !row(t, m, "draft.md").Saved || !row(t, m, "notes/more.md").Saved {
		t.Errorf("rows = %+v, want both drafts saved", m.Files)
	}
	e.wantSkipped(m, "go.mod", KindOther, SkipNotAuthored)
	e.wantSkipped(m, "package.json", KindData, SkipNotAuthored)
}

// Done-when 12: an oversize file, a binary file, a log and a secret match are
// listed with a skip reason and never copied.
func TestCapture_SkipsOversizeBinaryLogSecret(t *testing.T) {
	e := newEnv(t)
	e.write("big.md", strings.Repeat("x", MaxFileBytes+1))
	e.write("edge.md", strings.Repeat("x", MaxFileBytes))
	e.write("bin.txt", "abc\x00def")
	e.write("run.log", "line\n")
	e.write("creds.md", "use ghp_abcdefghijklmnopqrstuvwxyz0123 here\n")
	e.write("tool.py", "print(1)\n")
	e.write("shot.png", "\x89PNG\x00")

	m, _ := e.capture("")

	e.wantSkipped(m, "big.md", KindDocument, SkipOversize)
	e.wantSkipped(m, "bin.txt", KindDocument, SkipBinary)
	e.wantSkipped(m, "run.log", KindLog, SkipNotAuthored)
	e.wantSkipped(m, "creds.md", KindDocument, SkipSecret)
	e.wantSkipped(m, "tool.py", KindScript, SkipNotAuthored)
	e.wantSkipped(m, "shot.png", KindImage, SkipNotAuthored)
	if !row(t, m, "edge.md").Saved {
		t.Error("a file exactly at the size cap must be saved")
	}
}

// Each authored extension is saved, whatever its letter case.
func TestCapture_AuthoredKinds(t *testing.T) {
	e := newEnv(t)
	names := []string{"a.md", "b.txt", "c.html", "d.qmd", "e.rst", "F.MD"}
	for _, n := range names {
		e.write(n, n+"\n")
	}
	m, _ := e.capture("")
	for _, n := range names {
		if !row(t, m, n).Saved {
			t.Errorf("%s was not saved", n)
		}
	}
}

// A saved draft that later fails a check keeps its older copy, and the row
// says why the newer file was not copied.
func TestCapture_FailedCheckKeepsOlderCopy(t *testing.T) {
	e := newEnv(t)
	e.write("draft.md", "clean\n")
	first, _ := e.capture("")
	e.write("draft.md", "now with sk-abcdefghijklmnop inside\n")

	m, _ := e.capture("")

	r := row(t, m, "draft.md")
	if !r.Saved || r.SkipReason != SkipSecret || r.SHA256 != row(t, first, "draft.md").SHA256 {
		t.Errorf("row = %+v, want the older copy kept with skip reason secret", r)
	}
	if got, _ := os.ReadFile(e.copyPath("draft.md")); string(got) != "clean\n" {
		t.Errorf("saved copy = %q, want the older clean content", got)
	}
	if _, again := e.capture(""); again.Wrote {
		t.Error("an unchanged failing draft wrote the manifest again")
	}
}

// Done-when 15: a symbolic link is listed and never followed.
func TestCapture_SymlinkListedNeverFollowed(t *testing.T) {
	e := newEnv(t)
	outside := filepath.Join(filepath.Dir(e.pad), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret-plan.md"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret-plan.md"), filepath.Join(e.pad, "link.md")); err != nil {
		t.Skipf("no symlink support: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(e.pad, "linkdir")); err != nil {
		t.Fatal(err)
	}

	m, _ := e.capture("")

	e.wantSkipped(m, "link.md", KindDocument, SkipSymlink)
	e.wantSkipped(m, "linkdir", KindOther, SkipSymlink)
	if len(m.Files) != 2 {
		t.Errorf("rows = %+v, want only the two link rows", m.Files)
	}
	if _, err := os.Lstat(filepath.Join(e.sessionDir(), "files")); err == nil {
		t.Error("a link target was copied into the store")
	}
}

// A scratchpad path that is itself a link is not walked.
func TestCapture_LinkedScratchpadIsNotWalked(t *testing.T) {
	e := newEnv(t)
	e.write("draft.md", "x\n")
	link := filepath.Join(filepath.Dir(e.pad), "padlink")
	if err := os.Symlink(e.pad, link); err != nil {
		t.Skipf("no symlink support: %v", err)
	}
	res, err := Capture(Request{StoreDir: e.store, SessionID: testSession, ScratchpadDir: link})
	if err != nil || res.Wrote {
		t.Errorf("result = %+v, %v, want a no-op", res, err)
	}
	if _, err := os.Lstat(filepath.Join(e.store, "scratch")); err == nil {
		t.Error("a linked scratchpad left a scratch folder in the store")
	}
}

// A large subfolder that sorts first must not starve the top-level drafts:
// the walk reads the top level before any subfolder (seen live: a pass ended
// inside a copied store and never reached later drafts).
func TestCapture_TopLevelIsWalkedFirst(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 20; i++ {
		e.write(fmt.Sprintf("aaa/note-%02d.md", i), "inner\n")
	}
	e.write("zzz-draft.md", "mine\n")
	e.lim.maxFiles = 5

	m, res := e.capture("")

	if res.StoppedEarly != StopFileCount {
		t.Fatalf("stopped = %q, want the file count limit", res.StoppedEarly)
	}
	if !row(t, m, "zzz-draft.md").Saved {
		t.Error("the top-level draft was not saved before the subfolder used the budget")
	}
}

// Drafts are saved down to MaxSaveDepth folders. Deeper folders are never
// walked, and the folder that holds them gets one row for all of them.
func TestCapture_DeepFoldersShareOneRow(t *testing.T) {
	e := newEnv(t)
	e.write("a/b/kept.md", "kept\n")
	for i := 0; i < 30; i++ {
		e.write(fmt.Sprintf("a/b/c%02d/deep.md", i), "deep\n")
	}
	e.write("a/b/c00/d/deeper.md", "deeper\n")
	e.lim.maxFiles = 10 // thirty deep folders must not count against the walk

	m, res := e.capture("")

	if res.StoppedEarly != "" {
		t.Errorf("stopped = %q, want a full walk", res.StoppedEarly)
	}
	if !row(t, m, "a/b/kept.md").Saved {
		t.Error("a draft two folders down was not saved")
	}
	if r := row(t, m, "a/b"); r.Kind != KindTree || r.SkipReason != SkipDeep || r.Saved {
		t.Errorf("row = %+v, want one deep row for the folder", r)
	}
	if len(m.Files) != 2 || e.hasCopy("a/b/c00/deep.md") {
		t.Errorf("rows = %+v, want the draft and one deep row, no deep copy", m.Files)
	}
}

// A copied store and a tool home are trees: one row each, nothing copied.
func TestCapture_StoreCopyAndToolHomeAreTrees(t *testing.T) {
	e := newEnv(t)
	e.write("store-copy/sessions-ledger.html", "<html></html>\n")
	e.write("store-copy/bugs/bug-1.html", "<html></html>\n")
	e.write("cx-home/installation_id", "id\n")
	e.write("cx-home/skills/x/SKILL.md", "skill\n")
	e.write("fake-home/.claude/notes.md", "n\n")
	e.write("draft.md", "mine\n")

	m, _ := e.capture("")

	e.wantSkipped(m, "store-copy", KindTree, SkipTree)
	e.wantSkipped(m, "cx-home", KindTree, SkipTree)
	e.wantSkipped(m, "fake-home", KindTree, SkipTree)
	if len(m.Files) != 4 || !row(t, m, "draft.md").Saved {
		t.Errorf("rows = %+v, want three tree rows and the saved draft", m.Files)
	}
}

// Rows an older pass saved below a folder that is now one row keep their
// state: the walk no longer reads there, so it cannot call them gone.
func TestCapture_RowsBelowAPrunedFolderStayPresent(t *testing.T) {
	e := newEnv(t)
	e.write("copy/page.html", "<html></html>\n")
	e.capture("")
	e.write("copy/sessions-ledger.html", "<html></html>\n")

	m, _ := e.capture("")

	if r := row(t, m, "copy"); r.Kind != KindTree || r.SkipReason != SkipTree {
		t.Errorf("row = %+v, want one tree row", r)
	}
	if r := row(t, m, "copy/page.html"); r.State != StatePresent || !r.Saved {
		t.Errorf("row = %+v, want it kept as present and saved", r)
	}
}
