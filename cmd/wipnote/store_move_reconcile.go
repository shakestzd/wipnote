package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// storeRescueDir is the folder in a store's shadow root, beside .wipnote/,
// that keeps old-folder files the app store could not take.
const storeRescueDir = "rescued"

// oldStoreReconciler makes sure no file is lost when a moved store's old
// folder leaves a project (feat-b153fe64). The folder can hold more than the
// move copied: records tracked only on another branch, a linked worktree's
// own files, and files written after the copy.
type oldStoreReconciler struct {
	w       io.Writer
	owner   string // the moved store's shadow root
	rescue  string // this run's rescue folder, made on first use
	folders int    // old folders seen so far in this run
}

// reconcile accounts for every file of the old store folder src before the
// caller untracks or deletes it. gitTop is the checkout that holds src, or ""
// outside git. A file the app store lacks is copied in. A file it holds with
// other content is left to git history when HEAD has exactly that content,
// and is copied to the rescue folder otherwise. The app store's copy is never
// overwritten. On an error the caller must leave src and the index alone.
func (r *oldStoreReconciler) reconcile(gitTop, src string) error {
	if _, err := os.Stat(src); err != nil {
		return nil // only the index holds the store, and git history keeps that
	}
	files, err := storeMoveFiles(src)
	if err != nil {
		return err
	}
	r.folders++
	label := fmt.Sprintf("%d-%s", r.folders, filepath.Base(filepath.Dir(src)))
	var inHead map[string]bool
	added, rescued := 0, 0
	for _, rel := range files {
		from, to := filepath.Join(src, rel), filepath.Join(r.owner, ".wipnote", rel)
		copied, differs, err := copyOrCompare(from, to)
		if err != nil {
			return fmt.Errorf("store move: copy %s into the app store: %w; %s and the git index were left as they are", rel, err, src)
		}
		if copied {
			added++
		}
		if !differs {
			continue
		}
		if inHead == nil {
			inHead = unmodifiedTrackedFiles(gitTop, src)
		}
		if inHead[filepath.ToSlash(rel)] {
			continue
		}
		if err := r.rescueFile(from, filepath.Join(label, rel)); err != nil {
			return fmt.Errorf("store move: keep %s in the rescue folder: %w; %s and the git index were left as they are", rel, err, src)
		}
		rescued++
	}
	r.report(src, added, rescued)
	return nil
}

func (r *oldStoreReconciler) report(src string, added, rescued int) {
	if added > 0 {
		fmt.Fprintf(r.w, "Added %d file(s) from %s that the app store did not have.\n", added, src)
		if err := commitStoreFiles(r.owner, "Add records found in the project's old store folder"); err != nil {
			fmt.Fprintf(r.w, "warning: the files are copied but the store commit failed: %v\n", err)
		}
	}
	if rescued > 0 {
		fmt.Fprintf(r.w, "Kept %d file(s) from %s that differ from the app store's copies in %s. The app store's copies were not changed.\n", rescued, src, r.rescue)
	}
}

// copyOrCompare copies from to to when to does not exist. Otherwise it says
// whether the two files differ.
func copyOrCompare(from, to string) (copied, differs bool, err error) {
	if copied, err = copyNewFile(from, to); err != nil || copied {
		return copied, false, err
	}
	a, err := os.ReadFile(from)
	if err != nil {
		return false, false, err
	}
	b, err := os.ReadFile(to)
	if err != nil {
		return false, false, err
	}
	return false, !bytes.Equal(a, b), nil
}

// rescueFile copies from into this run's rescue folder at rel.
func (r *oldStoreReconciler) rescueFile(from, rel string) error {
	if r.rescue == "" {
		parent := filepath.Join(r.owner, storeRescueDir)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		dir, err := os.MkdirTemp(parent, time.Now().UTC().Format("20060102T150405Z")+"-")
		if err != nil {
			return err
		}
		r.rescue = dir
	}
	copied, err := copyNewFile(from, filepath.Join(r.rescue, rel))
	if err == nil && !copied {
		err = fmt.Errorf("%s already exists", filepath.Join(r.rescue, rel))
	}
	return err
}

// unmodifiedTrackedFiles is the set of files under src, relative to src with
// forward slashes, that git tracks in the checkout at top and whose working
// copy and index entry both match HEAD. It is empty outside git and whenever
// git cannot answer, so a file in doubt is rescued.
func unmodifiedTrackedFiles(top, src string) map[string]bool {
	set := map[string]bool{}
	rel, err := filepath.Rel(top, src)
	if top == "" || err != nil {
		return set
	}
	changed, err := gitRunner(top, "diff", "--name-only", "-z", "HEAD", "--", rel)
	if err != nil {
		return set
	}
	prefix := filepath.ToSlash(rel)
	for _, f := range trackedStoreFiles(top, rel) {
		if below, rerr := filepath.Rel(prefix, f); rerr == nil {
			set[path.Clean(filepath.ToSlash(below))] = true
		}
	}
	for _, f := range strings.FieldsFunc(string(changed), func(c rune) bool { return c == 0 }) {
		if below, rerr := filepath.Rel(prefix, f); rerr == nil {
			delete(set, path.Clean(filepath.ToSlash(below)))
		}
	}
	return set
}

// projectCheckouts lists the checkouts of the project's own repository that a
// run may take the old store out of: the main checkout and, when it is a
// linked worktree, the checkout the command runs in. A checkout of another
// repository (a submodule, a nested clone) is never touched. One notice
// names its store folder.
func projectCheckouts(w io.Writer, p storeMovePlan) []string {
	tops := []string{p.code}
	top := checkoutTop(p.cwd)
	if top == p.code {
		return tops
	}
	mine, merr := gitCommonDir(p.code)
	if theirs, terr := gitCommonDir(top); merr == nil && terr == nil && mine == theirs {
		return append(tops, top)
	}
	if store := nearestStoreFolder(p.cwd, top, p.rel); store != "" {
		fmt.Fprintf(w, "%s belongs to another git repository than %s. wipnote did not move it: it made no commit there and deleted nothing.\n", store, p.code)
	}
	return tops
}

// nearestStoreFolder is the first .wipnote folder from cwd up to top, else the
// folder at rel in top, else "".
func nearestStoreFolder(cwd, top, rel string) string {
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if store := filepath.Join(dir, ".wipnote"); isDirPath(store) {
			return store
		}
		if dir == top || dir == filepath.Dir(dir) {
			break
		}
	}
	if store := filepath.Join(top, rel); isDirPath(store) {
		return store
	}
	return ""
}
