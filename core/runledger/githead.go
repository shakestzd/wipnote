package runledger

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// HeadCommit returns the full HEAD sha of the git checkout containing dir, or
// "" when there is none. It reads .git files directly rather than running
// git, so a hook pays a few small file reads and no process start. It
// understands linked worktrees (a .git file naming the gitdir, whose
// commondir holds the refs) and packed refs.
func HeadCommit(dir string) string {
	gitDir := findGitDir(dir)
	if gitDir == "" {
		return ""
	}
	head := readTrimmed(filepath.Join(gitDir, "HEAD"))
	ref, ok := strings.CutPrefix(head, "ref: ")
	if !ok {
		return validSHA(head)
	}
	common := gitDir
	if c := readTrimmed(filepath.Join(gitDir, "commondir")); c != "" {
		if !filepath.IsAbs(c) {
			c = filepath.Join(gitDir, c)
		}
		common = c
	}
	for _, d := range []string{gitDir, common} {
		if sha := validSHA(readTrimmed(filepath.Join(d, filepath.FromSlash(ref)))); sha != "" {
			return sha
		}
	}
	return packedRef(filepath.Join(common, "packed-refs"), ref)
}

// findGitDir walks up from dir to the nearest .git, resolving a .git file
// ("gitdir: <path>") to the directory it names.
func findGitDir(dir string) string {
	if dir == "" {
		return ""
	}
	dir, _ = filepath.Abs(dir)
	for {
		p := filepath.Join(dir, ".git")
		if info, err := os.Stat(p); err == nil {
			if info.IsDir() {
				return p
			}
			if g, ok := strings.CutPrefix(readTrimmed(p), "gitdir: "); ok {
				if !filepath.IsAbs(g) {
					g = filepath.Join(dir, g)
				}
				return g
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func packedRef(path, ref string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		sha, name, ok := strings.Cut(sc.Text(), " ")
		if ok && name == ref {
			return validSHA(sha)
		}
	}
	return ""
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// validSHA returns s when it is a 40- or 64-hex object id, else "".
func validSHA(s string) string {
	if len(s) != 40 && len(s) != 64 {
		return ""
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return ""
		}
	}
	return s
}
