package scratch

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Request names one session's scratchpad and where to save it.
type Request struct {
	StoreDir      string // the store folder (.wipnote); Capture never creates it
	SessionID     string // names the folder under scratch/
	ScratchpadDir string // the harness scratchpad, from the hook payload
	WorkItem      string // the session's active work item, or ""
}

// Result reports what one pass did.
type Result struct {
	Saved        int    // drafts copied by this pass
	Wrote        bool   // the manifest changed on disk
	StoppedEarly string // StopTimeBudget, StopFileCount or ""
}

// Capture runs one bounded pass over the scratchpad. It copies new and
// changed authored documents into the store and updates the manifest. An
// empty or missing scratchpad, or a missing store, is a no-op and no error.
func Capture(req Request) (Result, error) {
	return capture(req, defaultLimits())
}

func capture(req Request, lim limits) (Result, error) {
	sessionDir, err := SessionDir(req.StoreDir, req.SessionID)
	if err != nil {
		return Result{}, err
	}
	root := filepath.Clean(req.ScratchpadDir)
	if req.ScratchpadDir == "" || !filepath.IsAbs(root) || !isRealDir(root) || !isDir(req.StoreDir) {
		return Result{}, nil
	}
	if _, err := os.Lstat(ManifestPath(sessionDir)); errors.Is(err, os.ErrNotExist) && dirEmpty(root) {
		return Result{}, nil // nothing to list: leave no folder behind
	}
	p := &pass{
		root: root, sessionDir: sessionDir, item: req.WorkItem, lim: lim,
		deadline: lim.now().Add(lim.budget),
	}
	wrote, err := update(sessionDir, func(m *Manifest) error {
		p.run(m)
		return nil
	})
	return Result{Saved: p.saved, Wrote: wrote, StoppedEarly: p.stopped}, err
}

// pass is the state of one walk, held under the manifest lock.
type pass struct {
	root       string
	sessionDir string
	item       string
	lim        limits
	deadline   time.Time

	rows    map[string]*Row // by path
	seen    map[string]bool // paths this walk met
	blind   []string        // folder prefixes the walk could not read
	expired map[string]bool // content hashes that retention dropped
	used    int64           // bytes of saved copies
	visited int
	saved   int
	stopped string
}

// run merges one walk into m. Rows the walk did not meet stay as they are.
func (p *pass) run(m *Manifest) {
	p.rows = make(map[string]*Row, len(m.Files))
	p.seen = map[string]bool{}
	p.expired = map[string]bool{}
	for i := range m.Files {
		r := m.Files[i]
		p.rows[r.Path] = &r
		if r.State == StateExpired && r.SHA256 != "" {
			p.expired[r.SHA256] = true
		}
		p.used += savedSize(&r)
	}
	p.walk()
	if p.stopped == "" {
		p.markGone() // only a full walk proves a file is gone
	}
	m.Version = ManifestVersion
	m.SessionID = filepath.Base(p.sessionDir)
	m.ScratchpadDir = p.root
	m.StoppedEarly = p.stopped
	m.Files = m.Files[:0]
	for _, r := range p.rows {
		m.Files = append(m.Files, *r)
	}
}

func (p *pass) walk() {
	_ = filepath.WalkDir(p.root, func(path string, d fs.DirEntry, err error) error {
		if path == p.root {
			if err != nil { // the scratchpad cannot be read: every old row stays
				p.blind = append(p.blind, "")
				return fs.SkipAll
			}
			return nil
		}
		rel, relErr := filepath.Rel(p.root, path)
		if relErr != nil || d == nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if err != nil { // a folder that cannot be read: its old rows stay
			p.blind = append(p.blind, rel+"/")
			return nil
		}
		if p.overLimit() {
			p.listUnreached(rel, d)
			return fs.SkipAll
		}
		p.visited++
		return p.visit(path, rel, d)
	})
}

// overLimit records which limit ended the walk, if one did.
func (p *pass) overLimit() bool {
	switch {
	case p.visited >= p.lim.maxFiles:
		p.stopped = StopFileCount
	case p.lim.now().After(p.deadline):
		p.stopped = StopTimeBudget
	}
	return p.stopped != ""
}

// visit lists one scratchpad entry and copies it when it is an authored
// document. A symbolic link is listed and never opened.
func (p *pass) visit(path, rel string, d fs.DirEntry) error {
	p.seen[rel] = true
	info, err := d.Info()
	if err != nil {
		return nil
	}
	switch {
	case d.IsDir():
		if !isTree(path, d.Name()) {
			return nil
		}
		p.skip(rel, KindTree, info, SkipTree)
		return fs.SkipDir
	case info.Mode()&fs.ModeSymlink != 0:
		p.skip(rel, kindOf(rel), info, SkipSymlink)
	case !info.Mode().IsRegular():
		p.skip(rel, KindOther, info, SkipNotAuthored)
	case kindOf(rel) != KindDocument:
		p.skip(rel, kindOf(rel), info, SkipNotAuthored)
	default:
		p.document(path, rel, info)
	}
	return nil
}

// markGone flags rows whose file a full walk did not meet. Saved copies stay.
func (p *pass) markGone() {
	for path, r := range p.rows {
		if r.State != StatePresent || p.seen[path] || p.isBlind(path) {
			continue
		}
		r.State = StateGone
	}
}

func (p *pass) isBlind(path string) bool {
	for _, prefix := range p.blind {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// isRealDir reports whether path is a folder and not a link to one.
func isRealDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}

// isDir reports whether path is a folder, through any link.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// dirEmpty reports whether dir has no entry. An unreadable dir counts as empty.
func dirEmpty(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return true
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	return errors.Is(err, io.EOF)
}

// stamp is the RFC 3339 form of a file time in the manifest.
func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
