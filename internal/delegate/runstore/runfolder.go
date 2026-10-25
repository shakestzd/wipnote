package runstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
)

// Run-folder file names: the feat-a1df60c2 contract that the
// codex-herdr-pane mod writes and its follower and viewer read. The mod
// writes label with no newline and start, end, exit and host with one;
// readers trim.
const (
	FileRawLog   = "raw.log"   // harness output as captured
	FileCleanLog = "clean.log" // the log with noise removed
	FileCleanMD  = "clean.md"  // the run as Markdown with safe fencing
	FileLastMD   = "last.md"   // the final answer
	FileUsage    = "usage"     // one-line token summary
	FileStart    = "start"     // unix seconds
	FileEnd      = "end"       // unix seconds
	FileExit     = "exit"      // int; its presence ends the run
	FileLabel    = "label"     // pane title
	FileHost     = "host"      // claude | herdr | none
	FileRunHTML  = "run.html"  // rendered page
)

// ContractFiles lists every run-folder file of the contract.
var ContractFiles = []string{
	FileRawLog, FileCleanLog, FileCleanMD, FileLastMD, FileUsage,
	FileStart, FileEnd, FileExit, FileLabel, FileHost, FileRunHTML,
}

// Hosts a run can name in its host file.
const (
	HostClaude = "claude"
	HostHerdr  = "herdr"
	HostNone   = "none"
)

// RunFolder is <root>/runs/<run-id>/.
type RunFolder struct {
	id  string
	dir string
}

// RunInfo is a run folder read back.
type RunInfo struct {
	ID       string
	Label    string
	Host     string
	Usage    string
	Start    time.Time
	End      time.Time
	Finished bool // the exit file exists
	Exit     int
}

// CreateRun makes the run folder for id (mode 0700). It refuses an invalid
// id and one whose folder already exists.
func (s *Store) CreateRun(id string) (*RunFolder, error) {
	if !ValidRunID(id) {
		return nil, fmt.Errorf("runstore: invalid run id %q", id)
	}
	runs := filepath.Join(s.root, runsDir)
	if err := s.privateMkdir(runs); err != nil {
		return nil, err
	}
	dir := filepath.Join(runs, id)
	if err := os.Mkdir(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("runstore: create run %s: %w", id, err)
	}
	return &RunFolder{id: id, dir: dir}, nil
}

// Run opens the existing run folder for id.
func (s *Store) Run(id string) (*RunFolder, error) {
	if !ValidRunID(id) {
		return nil, fmt.Errorf("runstore: invalid run id %q", id)
	}
	dir := filepath.Join(s.root, runsDir, id)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("runstore: no run %s", id)
	}
	return &RunFolder{id: id, dir: dir}, nil
}

// ID is the run id.
func (r *RunFolder) ID() string { return r.id }

// Dir is the run folder's path.
func (r *RunFolder) Dir() string { return r.dir }

// WriteFile replaces one contract file atomically (temp file, fsync,
// rename). Names outside ContractFiles are refused; exit is written only by
// Finish.
func (r *RunFolder) WriteFile(name string, data []byte) error {
	if !isContractFile(name) || name == FileExit {
		return fmt.Errorf("runstore: %q is not a writable run-folder file", name)
	}
	return filelock.WriteFileAtomic(filepath.Join(r.dir, name), data, filePerm)
}

// WriteLabel writes the pane title (one line, no newline, as the mod does).
func (r *RunFolder) WriteLabel(label string) error {
	label = strings.TrimSpace(strings.SplitN(label, "\n", 2)[0])
	return r.WriteFile(FileLabel, []byte(label))
}

// WriteHost writes claude, herdr or none.
func (r *RunFolder) WriteHost(host string) error {
	switch host {
	case HostClaude, HostHerdr, HostNone:
		return r.WriteFile(FileHost, []byte(host+"\n"))
	}
	return fmt.Errorf("runstore: invalid host %q", host)
}

// WriteUsage writes the one-line token summary.
func (r *RunFolder) WriteUsage(line string) error {
	line = strings.TrimSpace(strings.SplitN(line, "\n", 2)[0])
	return r.WriteFile(FileUsage, []byte(line+"\n"))
}

// WriteStart writes the start time in unix seconds.
func (r *RunFolder) WriteStart(t time.Time) error {
	return r.WriteFile(FileStart, unixLine(t))
}

// Finish ends the run: end first, then exit, so a reader that sees exit also
// sees end. exit goes through a temp file and rename as in the mod.
func (r *RunFolder) Finish(exit int, end time.Time) error {
	if err := r.WriteFile(FileEnd, unixLine(end)); err != nil {
		return err
	}
	data := []byte(strconv.Itoa(exit) + "\n")
	return filelock.WriteFileAtomic(filepath.Join(r.dir, FileExit), data, filePerm)
}

// Read returns the run's label, host, usage, times and exit.
func (r *RunFolder) Read() (RunInfo, error) {
	if fi, err := os.Lstat(r.dir); err != nil || !fi.IsDir() {
		return RunInfo{}, fmt.Errorf("runstore: no run %s", r.id)
	}
	info := RunInfo{
		ID:    r.id,
		Label: r.text(FileLabel),
		Host:  r.text(FileHost),
		Usage: r.text(FileUsage),
		Start: unixOf(r.text(FileStart)),
		End:   unixOf(r.text(FileEnd)),
	}
	exit, err := r.exitCode()
	if err == nil {
		info.Finished, info.Exit = true, exit
	} else if !errors.Is(err, os.ErrNotExist) {
		return info, err
	}
	return info, nil
}

// exitCode reads the exit file; os.ErrNotExist while the run is live.
func (r *RunFolder) exitCode() (int, error) {
	path := filepath.Join(r.dir, FileExit)
	if _, err := os.Lstat(path); err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(r.text(FileExit))
	if err != nil {
		return 1, nil // the mod reads an unreadable code as a failure
	}
	return n, nil
}

// text returns a trimmed contract file, empty when absent or a symlink.
func (r *RunFolder) text(name string) string {
	path := filepath.Join(r.dir, name)
	if !isPlainFile(path) {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func isContractFile(name string) bool {
	for _, n := range ContractFiles {
		if n == name {
			return true
		}
	}
	return false
}

func unixLine(t time.Time) []byte {
	return []byte(strconv.FormatInt(t.Unix(), 10) + "\n")
}

func unixOf(s string) time.Time {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}
