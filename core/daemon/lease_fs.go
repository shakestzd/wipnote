package daemon

import (
	"io/fs"
	"os"
)

// leaseFileOps is every file operation the lease and the socket guard make.
// It exists so a test can add latency to each call and prove the protocol
// still admits one owner on a slow bind mount (bug-5ca53f70). Production
// always uses osLeaseFS.
type leaseFileOps interface {
	ReadFile(name string) ([]byte, error)
	Stat(name string) (fs.FileInfo, error)
	Lstat(name string) (fs.FileInfo, error)
	// CreateExcl creates name with O_CREATE|O_EXCL and writes data. It fails
	// with an fs.ErrExist error when name already exists.
	CreateExcl(name string, data []byte) error
	Rename(oldpath, newpath string) error
	Link(oldname, newname string) error
	Remove(name string) error
}

// leaseFS is the file layer in use. Only tests replace it.
var leaseFS leaseFileOps = osLeaseFS{}

type osLeaseFS struct{}

func (osLeaseFS) ReadFile(name string) ([]byte, error)   { return os.ReadFile(name) }
func (osLeaseFS) Stat(name string) (fs.FileInfo, error)  { return os.Stat(name) }
func (osLeaseFS) Lstat(name string) (fs.FileInfo, error) { return os.Lstat(name) }
func (osLeaseFS) Rename(oldpath, newpath string) error   { return os.Rename(oldpath, newpath) }
func (osLeaseFS) Link(oldname, newname string) error     { return os.Link(oldname, newname) }
func (osLeaseFS) Remove(name string) error               { return os.Remove(name) }

func (osLeaseFS) CreateExcl(name string, data []byte) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(name)
	}
	return werr
}
