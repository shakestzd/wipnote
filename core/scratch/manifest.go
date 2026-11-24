package scratch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/shakestzd/wipnote/core/filelock"
)

// ManifestVersion is the manifest format this package writes.
const ManifestVersion = 1

// ErrUnsafeSessionID means the session id cannot name a folder safely.
var ErrUnsafeSessionID = errors.New("scratch: session id is not a safe folder name")

// Row describes one scratch file, or one project tree, in the manifest.
type Row struct {
	Path       string   `json:"path"`                  // relative to the scratchpad, slash-separated
	Kind       string   `json:"kind"`                  // one of the Kind constants
	Size       int64    `json:"size"`                  // bytes; of the saved copy when Saved
	MTime      string   `json:"mtime"`                 // RFC 3339
	SHA256     string   `json:"sha256,omitempty"`      // of the saved content
	Saved      bool     `json:"saved"`                 // a copy is in files/
	SkipReason string   `json:"skip_reason,omitempty"` // why the current file was not copied
	WorkItems  []string `json:"work_items"`            // items active when the draft changed
	State      string   `json:"state"`                 // one of the State constants
	Pinned     bool     `json:"pinned"`                // set by a later slice; Capture only keeps it
}

// Manifest is the content of <store>/scratch/<session>/manifest.json.
type Manifest struct {
	Version       int    `json:"version"`
	SessionID     string `json:"session_id"`
	ScratchpadDir string `json:"scratchpad_dir"`
	// StoppedEarly names the limit that ended the last pass before it saw
	// every file, or is empty when the pass finished.
	StoppedEarly string `json:"stopped_early,omitempty"`
	Files        []Row  `json:"files"`
}

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// SafeSessionID reports whether id can name the session's store folder: it
// holds no path separator and cannot climb out of scratch/.
func SafeSessionID(id string) bool {
	return safeSessionID.MatchString(id)
}

// SessionDir is <storeDir>/scratch/<sessionID>. It refuses an unsafe id.
func SessionDir(storeDir, sessionID string) (string, error) {
	if !SafeSessionID(sessionID) {
		return "", ErrUnsafeSessionID
	}
	return filepath.Join(storeDir, "scratch", sessionID), nil
}

// ManifestPath is the manifest file inside a session's scratch folder.
func ManifestPath(sessionDir string) string {
	return filepath.Join(sessionDir, "manifest.json")
}

// Load reads a session's manifest. A missing file is an empty manifest.
func Load(storeDir, sessionID string) (Manifest, error) {
	dir, err := SessionDir(storeDir, sessionID)
	if err != nil {
		return Manifest{}, err
	}
	m, _, err := load(ManifestPath(dir))
	return m, err
}

// Update applies fn to the session's manifest under the manifest's file lock
// and writes the result when it differs. Every manifest writer calls it, so a
// capture pass and another writer never drop each other's rows.
func Update(storeDir, sessionID string, fn func(*Manifest) error) error {
	dir, err := SessionDir(storeDir, sessionID)
	if err != nil {
		return err
	}
	_, err = update(dir, fn)
	return err
}

// update is Update for a resolved session folder. It reports whether it wrote.
func update(sessionDir string, fn func(*Manifest) error) (bool, error) {
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return false, fmt.Errorf("scratch: make session folder: %w", err)
	}
	path := ManifestPath(sessionDir)
	release := filelock.Guard(path)
	defer release()

	m, raw, err := load(path)
	if err != nil {
		return false, err
	}
	if err := fn(&m); err != nil {
		return false, err
	}
	out, err := encode(m)
	if err != nil {
		return false, err
	}
	if bytes.Equal(out, raw) {
		return false, nil
	}
	if err := filelock.WriteFileAtomic(path, out, 0o600); err != nil {
		return false, fmt.Errorf("scratch: write manifest: %w", err)
	}
	return true, nil
}

// load returns the manifest at path with its raw bytes. A file that does not
// parse is an error: writing over it would drop every row it held.
func load(path string) (Manifest, []byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, nil, nil
	}
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("scratch: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, nil, fmt.Errorf("scratch: parse manifest: %w", err)
	}
	return m, raw, nil
}

// encode renders m with rows sorted by path, so equal content gives equal
// bytes and an unchanged pass writes nothing.
func encode(m Manifest) ([]byte, error) {
	if m.Files == nil {
		m.Files = []Row{}
	}
	sort.SliceStable(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	for i := range m.Files {
		if m.Files[i].WorkItems == nil {
			m.Files[i].WorkItems = []string{}
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("scratch: encode manifest: %w", err)
	}
	return append(out, '\n'), nil
}
