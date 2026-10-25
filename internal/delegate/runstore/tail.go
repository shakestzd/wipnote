package runstore

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReadTail returns at most max bytes from the end of path, cut to start on a
// line boundary. When the window starts inside a line, that partial first
// line is dropped; when it starts exactly at a line's beginning (the byte
// before the window is a newline) the line is whole and kept. orch's tail
// read dropped through the first newline either way and so lost a whole line
// (orch finding F-4); the byte before the window is what tells the two apart.
// A file no larger than max is returned whole. Symlinks are refused.
func ReadTail(path string, max int64) ([]byte, error) {
	f, size, err := openPlain(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if size <= max {
		return readAt(f, 0, size)
	}
	off := size - max
	buf, err := readAt(f, off-1, max+1) // one byte before the window
	if err != nil {
		return nil, err
	}
	if len(buf) == 0 {
		return nil, nil
	}
	if buf[0] == '\n' {
		return buf[1:], nil
	}
	for i := 1; i < len(buf); i++ {
		if buf[i] == '\n' {
			return buf[i+1:], nil
		}
	}
	return nil, nil // the whole window is one partial line
}

// ReadFrom returns a run-folder file's bytes from off to its end, and its
// size, for a follower that reads only what is new. An off past the end (the
// file was replaced by a shorter one) returns no bytes and the new size.
func (r *RunFolder) ReadFrom(name string, off int64) ([]byte, int64, error) {
	if !isContractFile(name) {
		return nil, 0, fmt.Errorf("runstore: %q is not a run-folder file", name)
	}
	f, size, err := openPlain(filepath.Join(r.dir, name))
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	if off < 0 || off >= size {
		return nil, size, nil
	}
	data, err := readAt(f, off, size-off)
	return data, size, err
}

// Tail is ReadTail on a run-folder file.
func (r *RunFolder) Tail(name string, max int64) ([]byte, error) {
	if !isContractFile(name) {
		return nil, fmt.Errorf("runstore: %q is not a run-folder file", name)
	}
	return ReadTail(filepath.Join(r.dir, name), max)
}

// openPlain opens a regular file (never a symlink) and returns its size.
func openPlain(path string) (*os.File, int64, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !fi.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("runstore: %s is not a plain file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// readAt reads n bytes at off; a file that shrank meanwhile gives fewer.
func readAt(f *os.File, off, n int64) ([]byte, error) {
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:got], nil
}
