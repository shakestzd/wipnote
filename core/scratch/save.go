package scratch

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/secretscan"
)

// document handles one authored document: it copies the file when it is new
// or changed and passes every check.
func (p *pass) document(path, rel string, info fs.FileInfo) {
	old := p.rows[rel]
	if old != nil && unchanged(old, info) {
		return
	}
	if info.Size() > p.lim.maxFile {
		p.skip(rel, KindDocument, info, SkipOversize)
		return
	}
	if p.used-savedSize(old)+info.Size() > p.lim.maxSession {
		p.skip(rel, KindDocument, info, SkipSessionCap)
		return
	}
	data, ok := readRegular(path, info, p.lim.maxFile)
	if !ok {
		return // unreadable or swapped under us: make no claim about it
	}
	switch {
	case int64(len(data)) > p.lim.maxFile:
		p.skip(rel, KindDocument, info, SkipOversize)
	case isBinary(data):
		p.skip(rel, KindDocument, info, SkipBinary)
	case secretscan.Match(data):
		p.skip(rel, KindDocument, info, SkipSecret)
	default:
		p.keep(rel, info, data)
	}
}

// keep saves data as the copy of rel, unless the store already holds that
// content or retention already dropped it.
func (p *pass) keep(rel string, info fs.FileInfo, data []byte) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	old := p.rows[rel]
	if p.expired[hash] {
		p.listExpired(rel, info, hash)
		return
	}
	if old != nil && old.Saved && old.SHA256 == hash {
		// Same content as the saved copy: only the file's time moved.
		old.MTime, old.State, old.SkipReason = stamp(info.ModTime()), StatePresent, ""
		return
	}
	dst := filepath.Join(p.sessionDir, "files", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return
	}
	if err := filelock.WriteFileAtomic(dst, data, 0o600); err != nil {
		return
	}
	p.used += int64(len(data)) - savedSize(old)
	p.saved++
	row := Row{
		Path: rel, Kind: KindDocument, Size: int64(len(data)), MTime: stamp(info.ModTime()),
		SHA256: hash, Saved: true, State: StatePresent, WorkItems: p.workItems(old),
	}
	if old != nil {
		row.Pinned = old.Pinned
	}
	p.rows[rel] = &row
}

// workItems is old's list plus the active item, each item once.
func (p *pass) workItems(old *Row) []string {
	items := []string{}
	if old != nil {
		items = append(items, old.WorkItems...)
	}
	if p.item == "" {
		return items
	}
	for _, it := range items {
		if it == p.item {
			return items
		}
	}
	return append(items, p.item)
}

// skip lists rel as not copied. A row with a saved copy keeps that copy's
// size, time and hash, and gains the reason the newer file was not copied:
// an older draft is worth more than none. An expired row is left alone.
func (p *pass) skip(rel, kind string, info fs.FileInfo, reason string) {
	old := p.rows[rel]
	if old != nil && old.State == StateExpired {
		return
	}
	if old != nil && old.Saved {
		old.SkipReason, old.State = reason, StatePresent
		return
	}
	row := Row{Path: rel, Kind: kind, MTime: stamp(info.ModTime()), SkipReason: reason, State: StatePresent, WorkItems: []string{}}
	if !info.IsDir() {
		row.Size = info.Size()
	}
	if old != nil {
		row.Pinned = old.Pinned
		row.WorkItems = append(row.WorkItems, old.WorkItems...)
	}
	p.rows[rel] = &row
}

// listExpired records that rel holds content retention already dropped. The
// row carries the hash so the next pass can tell without reading the file.
func (p *pass) listExpired(rel string, info fs.FileInfo, hash string) {
	if old := p.rows[rel]; old != nil && old.State == StateExpired {
		return
	}
	p.rows[rel] = &Row{
		Path: rel, Kind: KindDocument, Size: info.Size(), MTime: stamp(info.ModTime()),
		SHA256: hash, State: StateExpired, WorkItems: []string{},
	}
}

// listUnreached lists the entry the walk stopped at, when it has no row yet,
// so the manifest shows where the pass ended.
func (p *pass) listUnreached(rel string, d fs.DirEntry) {
	if p.rows[rel] != nil {
		return
	}
	row := Row{Path: rel, Kind: kindOf(rel), SkipReason: SkipBudget, State: StatePresent, WorkItems: []string{}}
	if d.IsDir() {
		row.Kind = KindOther
	}
	if info, err := d.Info(); err == nil {
		row.MTime = stamp(info.ModTime())
		if !info.IsDir() {
			row.Size = info.Size()
		}
	}
	p.rows[rel] = &row
}

// unchanged reports whether the file still matches its row, so the pass need
// not read it. A row skipped for budget or the session cap is tried again.
func unchanged(old *Row, info fs.FileInfo) bool {
	if old.Size != info.Size() || old.MTime != stamp(info.ModTime()) {
		return false
	}
	if old.State == StateExpired {
		return true
	}
	return old.State == StatePresent && old.SkipReason != SkipBudget && old.SkipReason != SkipSessionCap
}

// savedSize is the bytes row's saved copy holds in the store.
func savedSize(r *Row) int64 {
	if r == nil || !r.Saved || r.State == StateExpired {
		return 0
	}
	return r.Size
}

// readRegular reads at most max+1 bytes of the regular file that info
// describes. It refuses a path that became a link or another file since the
// walk listed it, so the pass never follows a link.
func readRegular(path string, info fs.FileInfo, max int64) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, false
	}
	return data, true
}
