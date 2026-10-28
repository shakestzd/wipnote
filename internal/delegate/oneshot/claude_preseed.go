package oneshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// preseedLockWait bounds the wait for Claude Code's own config lock.
var preseedLockWait = 5 * time.Second

// preseedBeforeRename is a test seam: it runs after the new config is
// written beside the old one and before the re-check and the rename.
var preseedBeforeRename func()

// preseedTries bounds the re-reads when the config changes under the seed.
const preseedTries = 3

// preseedClaude marks onboarding done and each dir's workspace trust
// accepted in the global config, so no dialog blocks the first prompt
// (the spike saw the positional prompt dropped behind them). It takes the
// lock Claude Code itself writes the file under (a <config>.lock folder,
// seen on claude 2.1.312), keeps every other key and every number exactly,
// keeps the file's mode, and writes only when a key was missing. A config
// that changed between the read and the rename is read again and the seed
// re-applied. A held lock that does not free, or a config that cannot be
// parsed, is left alone and reported.
func preseedClaude(path string, dirs ...string) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	unlock, err := lockClaudeConfig(path)
	if err != nil {
		return err
	}
	defer unlock()
	for try := 0; try < preseedTries; try++ {
		if done, err := seedOnce(path, dirs); done || err != nil {
			return err
		}
	}
	return fmt.Errorf("claude config %s kept changing; trust was not pre-seeded", path)
}

// lockClaudeConfig takes <path>.lock as Claude Code does (mkdir), waiting
// up to preseedLockWait. It never breaks another holder's lock.
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
// before the rename, nothing was written, try again.
func seedOnce(path string, dirs []string) (bool, error) {
	before := stampOf(path)
	cfg, err := readClaudeConfig(path)
	if err != nil {
		return false, err
	}
	if !seedKeys(cfg, dirs) {
		return true, nil
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	mode := before.mode
	if !before.exists {
		mode = 0o600
	}
	tmp, err := writeTemp(path, append(out, '\n'), mode)
	if err != nil {
		return false, err
	}
	if preseedBeforeRename != nil {
		preseedBeforeRename()
	}
	if stampOf(path) != before {
		_ = os.Remove(tmp)
		return false, nil
	}
	return true, os.Rename(tmp, path)
}

// readConfig decodes the config with numbers kept as written.
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
	return cfg, nil
}

func writeTemp(path string, data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.wipnote.*")
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
