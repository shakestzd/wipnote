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

// folder is one scratchpad folder waiting for its turn in the walk.
type folder struct {
	path  string
	rel   string // slash-separated, with a trailing slash; "" for the scratchpad
	depth int    // folders between it and the scratchpad
}

// walk visits the scratchpad level by level, so every top-level draft is
// saved before any subfolder is read. A depth-first walk spent its whole
// budget inside one large subfolder and never reached the drafts that sorted
// after it.
func (p *pass) walk() {
	queue := []folder{{path: p.root}}
	for len(queue) > 0 && p.stopped == "" {
		f := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(f.path)
		if err != nil { // a folder that cannot be read: its old rows stay
			p.blind = append(p.blind, f.rel)
			continue
		}
		queue = append(queue, p.visitFolder(f, entries)...)
	}
}

// visitFolder lists the entries of one folder and returns the subfolders to
// walk next.
func (p *pass) visitFolder(f folder, entries []fs.DirEntry) (next []folder) {
	for _, d := range entries {
		rel := f.rel + d.Name()
		if p.overLimit() {
			p.listUnreached(rel, d)
			return nil
		}
		p.visited++
		path := filepath.Join(f.path, d.Name())
		if p.visit(path, rel, d, f.depth) {
			next = append(next, folder{path: path, rel: rel + "/", depth: f.depth + 1})
		}
	}
	return next
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
// document. A symbolic link is listed and never opened. It reports whether
// the entry is a folder to walk. depth is the depth of the folder that holds
// the entry.
func (p *pass) visit(path, rel string, d fs.DirEntry, depth int) (descend bool) {
	p.seen[rel] = true
	info, err := d.Info()
	if err != nil {
		return false
	}
	switch {
	case d.IsDir():
		reason := ""
		switch {
		case isTree(path, d.Name()):
			reason = SkipTree
		case depth >= p.lim.maxDepth:
			reason = SkipDeep
		default:
			return true
		}
		p.skip(rel, KindTree, info, reason)
		// Rows an earlier pass wrote below this folder stay as they are: the
		// walk no longer looks there, so it cannot call them gone.
		p.blind = append(p.blind, rel+"/")
	case info.Mode()&fs.ModeSymlink != 0:
		p.skip(rel, kindOf(rel), info, SkipSymlink)
	case !info.Mode().IsRegular():
		p.skip(rel, KindOther, info, SkipNotAuthored)
	case kindOf(rel) != KindDocument:
		p.skip(rel, kindOf(rel), info, SkipNotAuthored)
	default:
		p.document(path, rel, info)
	}
	return false
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
