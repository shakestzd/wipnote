package oneshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// preseedLockWait bounds the wait for Claude Code's own config lock.
var preseedLockWait = 5 * time.Second

// preseedStaleLock is the age past which a lock folder counts as left over.
// A config write takes far less. The seed does not wait on such a lock, because
// every launch would then pay the whole preseedLockWait, and it never removes one.
var preseedStaleLock = 30 * time.Second

// preseedOldTemp is the age past which a temp file from a killed seed is removed.
const preseedOldTemp = time.Minute

// preseedBeforeRename is a test seam: it runs after the new config is
// written beside the old one and before the re-check and the rename.
var preseedBeforeRename func()

// preseedTries bounds the re-reads when the config changes under the seed.
const preseedTries = 3

// preseedClaude marks onboarding done and each dir's workspace trust
// accepted in the global config, so no dialog blocks the first prompt
// (the spike saw the positional prompt dropped behind them). It takes the
// lock Claude Code itself writes the file under (a <config>.lock folder,
// seen on claude 2.1.312), copies every other key and value as the file holds
// it (seedConfig), keeps the file's mode, and writes only when a key was
// missing. A config that changed between the read and the rename is read again
// and the seed re-applied. A held lock that does not free, a config that is not
// one JSON object, a "projects" value that is not an object, and a config link
// whose target is missing are all left alone and reported.
func preseedClaude(path string, dirs ...string) error {
	_, err := seedLocked(path, true, dirs)
	return err
}

// seedLocked is the locked, re-read seed. onboarding false sets trust only.
// wrote reports whether this call changed the file.
func seedLocked(path string, onboarding bool, dirs []string) (wrote bool, err error) {
	real, lerr := filepath.EvalSymlinks(path)
	switch {
	case lerr == nil:
		path = real
	case isSymlink(path):
		// The rename would replace the link with a near-empty file and orphan
		// the real config when its volume comes back.
		return false, fmt.Errorf("claude config %s is a link whose target is missing; nothing was changed", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	unlock, err := lockClaudeConfig(path)
	if err != nil {
		return false, err
	}
	defer unlock()
	removeOldTemps(path)
	for try := 0; try < preseedTries; try++ {
		if done, wrote, err := seedOnce(path, dirs, onboarding); done || err != nil {
			return wrote, err
		}
	}
	return false, fmt.Errorf("claude config %s kept changing; trust was not pre-seeded", path)
}

func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// removeOldTemps removes temp files a killed seed left beside the config. Each
// holds a full copy of the config. The caller holds the config lock.
func removeOldTemps(path string) {
	old, _ := filepath.Glob(path + tempSuffix + "*")
	for _, f := range old {
		if fi, err := os.Stat(f); err == nil && time.Since(fi.ModTime()) > preseedOldTemp {
			_ = os.Remove(f)
		}
	}
}

// tempSuffix follows the config's name in the seed's temp file.
const tempSuffix = ".tmp.wipnote."

// PreseedTrust marks dir trusted in Claude Code's global config (the same
// seed the one-shot interactive path does), for a spawn that runs a worker
// in a folder of its own.
func PreseedTrust(dir string) error {
	return preseedClaude(ClaudeConfigPath(), filepath.Clean(dir))
}

// TrustFolder sets only dir's trust flag in Claude Code's global config and
// no other key (bug-65bf54aa: store trust must not skip a user's onboarding).
// wrote false with no error means the flag was already set, for example by a
// launch that ran at the same time.
func TrustFolder(dir string) (wrote bool, err error) {
	abs, err := absFolder(dir)
	if err != nil {
		return false, err
	}
	return seedLocked(ClaudeConfigPath(), false, []string{abs})
}

// absFolder refuses an empty folder and makes a relative one absolute: Claude
// Code keys trust by absolute path, and a "." key would match every relative path.
func absFolder(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("claude trust: no folder given")
	}
	return filepath.Abs(dir)
}

// lockClaudeConfig takes <path>.lock as Claude Code does (mkdir), waiting
// up to preseedLockWait. It never breaks another holder's lock, and it does not
// wait on one old enough to be left over.
func lockClaudeConfig(path string) (func(), error) {
	lock := path + ".lock"
	deadline := time.Now().Add(preseedLockWait)
	for {
		err := os.Mkdir(lock, 0o777)
		if err == nil {
			return func() { _ = os.Remove(lock) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if fi, serr := os.Stat(lock); serr == nil && time.Since(fi.ModTime()) > preseedStaleLock {
			return nil, fmt.Errorf("claude config lock %s is %s old and looks left over; remove that folder if no claude is running. Trust was not pre-seeded",
				lock, time.Since(fi.ModTime()).Round(time.Second))
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("claude config lock %s is held (another claude is writing it); trust was not pre-seeded", lock)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// fileStamp is what tells a config write by someone else.
type fileStamp struct {
	exists bool
	size   int64
	mod    time.Time
	mode   os.FileMode
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{exists: true, size: fi.Size(), mod: fi.ModTime(), mode: fi.Mode().Perm()}
}

// seedOnce is one read, seed and rename. done false: the file changed
// before the rename, nothing was written, try again. wrote true: the rename
// put a changed config in place.
func seedOnce(path string, dirs []string, onboarding bool) (done, wrote bool, err error) {
	before := stampOf(path)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, false, err
	}
	out, changed, err := seedConfig(data, dirs, onboarding)
	if err != nil {
		return false, false, fmt.Errorf("claude config %s: %w; nothing was changed", path, err)
	}
	if !changed {
		return true, false, nil
	}
	mode := before.mode
	if !before.exists {
		mode = 0o600
	}
	tmp, err := writeTemp(path, out, mode)
	if err != nil {
		return false, false, err
	}
	if preseedBeforeRename != nil {
		preseedBeforeRename()
	}
	if stampOf(path) != before {
		_ = os.Remove(tmp)
		return false, false, nil
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, false, err
	}
	return true, true, nil
}

// readClaudeConfig decodes the config for a read-only question. It refuses data
// after the first JSON value, as the seed does.
func readClaudeConfig(path string) (map[string]any, error) {
	cfg := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("claude config %s: %w; nothing was changed", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("claude config %s: data follows the JSON object; nothing was changed", path)
	}
	if cfg == nil { // a config holding JSON null decodes to a nil map
		return nil, fmt.Errorf("claude config %s holds null, not an object; nothing was changed", path)
	}
	return cfg, nil
}

func writeTemp(path string, data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+tempSuffix+"*")
	if err != nil {
		return "", err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Chmod(mode)
	}
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(f.Name())
		return "", werr
	}
	return f.Name(), nil
}

// TrustAccepted reports whether Claude Code's global config marks dir itself
// as trusted. A trusted folder above dir does not count. Callers pass an app
// store, which is its own git repository, and Claude Code's permissions docs
// say trust on a folder does not cover a repository nested inside it. They also
// say trust on the home folder lasts one session. An earlier version accepted
// any trusted parent, so a trusted home folder made `wipnote store trust` a
// no-op while Claude Code went on asking (bug-65bf54aa). A missing config is
// "not trusted", not an error.
func TrustAccepted(dir string) (bool, error) {
	abs, err := absFolder(dir)
	if err != nil {
		return false, err
	}
	cfg, err := readClaudeConfig(ClaudeConfigPath())
	if err != nil {
		return false, err
	}
	projects, _ := cfg["projects"].(map[string]any)
	p, _ := projects[abs].(map[string]any)
	return p["hasTrustDialogAccepted"] == true, nil
}
