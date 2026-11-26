package oneshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claudeWritten is a config in the layout Claude Code writes: two spaces, keys
// in its own order, no trailing newline. It holds what a decode and re-encode
// would change: a lone surrogate escape, HTML characters, an escaped slash, a
// number past float range and an unsorted key order.
const claudeWritten = `{
  "numStartups": 12345678901234567890,
  "zeta": "cut emoji \ud83d and <b>&</b> and a\/b",
  "ratio": 1.0,
  "empty": {},
  "list": [],
  "projects": {
    "/z/other": {
      "allowedTools": [],
      "hasTrustDialogAccepted": false,
      "history": [
        {
          "display": "x"
        }
      ]
    },
    "/a/first": {
      "x": 1e21
    }
  },
  "alpha": null
}`

// The seed adds its own lines and changes no other byte of a config Claude Code
// wrote.
func TestSeedConfig_ChangesOnlyItsOwnLines(t *testing.T) {
	out, changed, err := seedConfig([]byte(claudeWritten), []string{"/s/store"}, false)
	if err != nil || !changed {
		t.Fatalf("changed %v err %v", changed, err)
	}
	want := strings.Replace(claudeWritten, `      "x": 1e21
    }
  },`, `      "x": 1e21
    },
    "/s/store": {
      "hasTrustDialogAccepted": true
    }
  },`, 1)
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	// An entry that exists keeps its place and its other keys.
	out, _, err = seedConfig([]byte(claudeWritten), []string{"/z/other"}, false)
	must(t, err)
	if want := strings.Replace(claudeWritten, `"hasTrustDialogAccepted": false`, `"hasTrustDialogAccepted": true`, 1); string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if _, changed, err := seedConfig(out, []string{"/z/other"}, false); changed || err != nil {
		t.Fatalf("a second seed changed the config: %v %v", changed, err)
	}
}

// A new or empty config gets the keys and a final newline. A path with HTML
// characters is written as it is.
func TestSeedConfig_EmptyConfigAndOddPath(t *testing.T) {
	out, changed, err := seedConfig(nil, []string{`/s/a<b>&"c`}, true)
	if err != nil || !changed {
		t.Fatalf("changed %v err %v", changed, err)
	}
	want := "{\n  \"hasCompletedOnboarding\": true,\n  \"projects\": {\n    \"/s/a<b>&\\\"c\": {\n      \"hasTrustDialogAccepted\": true\n    }\n  }\n}\n"
	if string(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// A config the seed cannot edit safely is refused and left byte for byte.
func TestTrustFolder_RefusesWhatItCannotKeep(t *testing.T) {
	for name, body := range map[string]string{
		"projects is an array":  `{"projects":["a"]}`,
		"projects is a string":  `{"projects":"x"}`,
		"projects is null":      `{"projects":null}`,
		"the entry is a string": `{"projects":{"/s/store":"x"}}`,
		"data after the object": `{"a":1} GARBAGE`,
		"a second object":       `{"a":1}{"b":2}`,
		"an array at the top":   `[1]`,
		"null at the top":       `null`,
		"a cut file":            `{"a":`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			path := filepath.Join(dir, ".claude.json")
			must(t, os.WriteFile(path, []byte(body), 0o600))
			wrote, err := seedLocked(path, false, []string{"/s/store"})
			if err == nil || wrote || !strings.Contains(err.Error(), "nothing was changed") {
				t.Fatalf("wrote %v err %v", wrote, err)
			}
			if data, _ := os.ReadFile(path); string(data) != body {
				t.Fatalf("config changed to %s", data)
			}
			if left, _ := filepath.Glob(path + "*"); len(left) != 1 {
				t.Fatalf("files left beside the config: %v", left)
			}
		})
	}
}

// A config link whose target is missing is not replaced by a new file.
func TestSeed_DanglingConfigLinkIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	path, target := filepath.Join(dir, ".claude.json"), filepath.Join(dir, "unmounted", "claude.json")
	must(t, os.Symlink(target, path))
	wrote, err := seedLocked(path, false, []string{"/s/store"})
	if err == nil || wrote || !strings.Contains(err.Error(), "target is missing") {
		t.Fatalf("wrote %v err %v", wrote, err)
	}
	if got, lerr := os.Readlink(path); lerr != nil || got != target {
		t.Fatalf("the link was replaced: %q %v", got, lerr)
	}
	// With the target present the seed writes through the link and keeps it.
	must(t, os.MkdirAll(filepath.Dir(target), 0o700))
	must(t, os.WriteFile(target, []byte(`{}`), 0o600))
	if wrote, err := seedLocked(path, false, []string{"/s/store"}); err != nil || !wrote {
		t.Fatalf("wrote %v err %v", wrote, err)
	}
	if _, lerr := os.Readlink(path); lerr != nil || !trusted(readConfig(t, target), "/s/store") {
		t.Fatalf("link kept: %v", lerr)
	}
}

// A lock folder old enough to be left over fails at once. The seed does not
// wait the whole lock time on it, and does not remove it.
func TestSeed_LeftOverLockFailsFast(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	must(t, os.WriteFile(path, []byte(`{}`), 0o600))
	lock := path + ".lock"
	must(t, os.Mkdir(lock, 0o777))
	old := time.Now().Add(-10 * time.Minute)
	must(t, os.Chtimes(lock, old, old))
	start := time.Now()
	_, err := seedLocked(path, false, []string{"/s/store"})
	if err == nil || !strings.Contains(err.Error(), "looks left over") {
		t.Fatalf("err %v", err)
	}
	if took := time.Since(start); took > preseedLockWait/2 {
		t.Fatalf("waited %s on a left-over lock", took)
	}
	if _, serr := os.Stat(lock); serr != nil {
		t.Fatalf("the lock was removed: %v", serr)
	}
}

// A temp file a killed seed left is removed once it is old. A fresh one, which
// may belong to a seed still running, is kept.
func TestSeed_RemovesOldTempFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	must(t, os.WriteFile(path, []byte(`{}`), 0o600))
	oldTemp, fresh := path+tempSuffix+"111", path+tempSuffix+"222"
	must(t, os.WriteFile(oldTemp, []byte(`{}`), 0o600))
	must(t, os.WriteFile(fresh, []byte(`{}`), 0o600))
	past := time.Now().Add(-time.Hour)
	must(t, os.Chtimes(oldTemp, past, past))
	wrote, err := seedLocked(path, false, []string{"/s/store"})
	if err != nil || !wrote {
		t.Fatalf("wrote %v err %v", wrote, err)
	}
	if _, serr := os.Stat(oldTemp); !os.IsNotExist(serr) {
		t.Fatal("the old temp file was kept")
	}
	if _, serr := os.Stat(fresh); serr != nil {
		t.Fatal("a fresh temp file was removed")
	}
	// wrote is false when the flag is already set.
	if wrote, err := seedLocked(path, false, []string{"/s/store"}); err != nil || wrote {
		t.Fatalf("second seed: wrote %v err %v", wrote, err)
	}
}
