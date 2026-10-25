package htmlledger

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AppendRow appends one marshalled row line to the ledger at path, creating
// the file (and its directory) with the header when needed. The caller must
// hold the write guard for path.
//
// An append is O(row), never a rewrite. A crash mid-write leaves a torn tail,
// so before writing it finds a known-good boundary: the footer if the file
// ends with it, otherwise the last newline, which is by construction the end
// of the last complete row. The write is fsynced before returning.
func (f Format) AppendRow(path string, row []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s: mkdir %s: %w", f.Name, filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("%s: open %s: %w", f.Name, path, err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%s: stat %s: %w", f.Name, path, err)
	}
	off, err := f.appendOffset(file, fi.Size())
	if err != nil {
		return fmt.Errorf("%s: %s: %w", f.Name, path, err)
	}

	var payload []byte
	if off == 0 {
		// Empty, or truncated to less than a full header: no complete row can
		// exist, so rewrite the whole document rather than guess at a boundary.
		payload = append(payload, f.Header...)
	}
	payload = append(payload, row...)
	payload = append(payload, f.Footer...)

	// Truncate FIRST so a crash between the two leaves a file that ends on a
	// clean row boundary with no footer — which the next append repairs — rather
	// than one with stale bytes past the new content.
	if err := file.Truncate(off); err != nil {
		return fmt.Errorf("%s: truncate %s: %w", f.Name, path, err)
	}
	if _, err := file.Seek(off, io.SeekStart); err != nil {
		return fmt.Errorf("%s: seek %s: %w", f.Name, path, err)
	}
	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("%s: write %s: %w", f.Name, path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("%s: fsync %s: %w", f.Name, path, err)
	}
	return nil
}

// appendOffset returns the byte offset at which a new row line may begin.
//
//   - size 0, or smaller than the header: 0 (rewrite the document).
//   - file ends with the exact footer: the footer's start (the clean case).
//   - otherwise the write was torn: the byte after the last newline, dropping
//     the incomplete tail. Every earlier row is preserved; only the fragment
//     that was never fsynced is lost.
func (f Format) appendOffset(file *os.File, size int64) (int64, error) {
	if size < int64(len(f.Header)) {
		return 0, nil
	}

	footerLen := int64(len(f.Footer))
	tail := make([]byte, footerLen)
	if _, err := file.ReadAt(tail, size-footerLen); err != nil {
		return 0, fmt.Errorf("read trailing bytes: %w", err)
	}
	if bytes.Equal(tail, []byte(f.Footer)) {
		return size - footerLen, nil
	}

	window := int64(tornScanWindow)
	if window > size {
		window = size
	}
	buf := make([]byte, window)
	if _, err := file.ReadAt(buf, size-window); err != nil {
		return 0, fmt.Errorf("read repair window: %w", err)
	}
	nl := bytes.LastIndexByte(buf, '\n')
	if nl < 0 {
		return 0, fmt.Errorf("torn ledger has no line boundary within %d bytes of the tail", window)
	}
	boundary := size - window + int64(nl) + 1
	// Never rewind into the header: a boundary inside it means the file was torn
	// during creation and holds no complete row.
	if boundary < int64(len(f.Header)) {
		return 0, nil
	}
	return boundary, nil
}

// WriteAll rewrites path with exactly rows (already marshalled and in the
// order to write), atomically via temp-then-rename. This is the close path:
// changing an existing row is a read-modify-write. The caller must hold the
// write guard for path.
func (f Format) WriteAll(path string, rows [][]byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s: mkdir %s: %w", f.Name, dir, err)
	}

	var buf bytes.Buffer
	buf.WriteString(f.Header)
	for _, row := range rows {
		buf.Write(row)
	}
	buf.WriteString(f.Footer)

	tmp, err := os.CreateTemp(dir, f.TempPattern)
	if err != nil {
		return fmt.Errorf("%s: create temp in %s: %w", f.Name, dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("%s: write temp: %w", f.Name, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("%s: fsync temp: %w", f.Name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s: close temp: %w", f.Name, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("%s: rename temp over %s: %w", f.Name, path, err)
	}
	return nil
}
