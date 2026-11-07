package paths

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The wipnote application owns the store, the way Claude Code keeps transcripts
// under ~/.claude/projects: a project uses wipnote with nothing added to its
// folder. The store's shadow root, ~/.wipnote/projects/<encoded path>/, holds
// .wipnote/ and project.json and is its own git repository, so every
// "<owner>/.wipnote" path and every artifact commit works unchanged. Two roots
// then exist: the store owner (the parent of .wipnote/) and the code root (the
// folder the user works in). CodeRoot maps the first to the second.
//
// ~/.wipnote itself is the application's home. It holds store-shaped folders
// but is never a project store, so $HOME is never a store owner.

// ProjectFileName is the marker file at the top of a shadow root.
const ProjectFileName = "project.json"

// externalStoreKind tells a shadow root's project.json from any other file of
// that name, which an ordinary repo is free to hold at its top level.
const externalStoreKind = "wipnote-external-store"

type projectFile struct {
	Kind     string `json:"kind"`
	CodeRoot string `json:"code_root"`
	// Origin says who made the store: OriginInit for an explicit `wipnote init`,
	// OriginLauncher for a launcher that made one on its own. Walking up from a
	// non-git folder honours only explicit stores. Absent reads as a launcher's.
	Origin string `json:"origin,omitempty"`
}

// Store origins recorded in project.json.
const (
	OriginInit     = "init"
	OriginLauncher = "launcher"
)

// DataDir is the application's home, ~/.wipnote.
func DataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wipnote"
	}
	return filepath.Join(home, ".wipnote")
}

// ProjectsDir is the folder holding every shadow root.
func ProjectsDir() string { return filepath.Join(DataDir(), "projects") }

// maxShadowName bounds a shadow root's folder name; most filesystems stop at 255
// and the store's temp name during creation is a little longer.
const maxShadowName = 200

// encodeProjectPath names a project's shadow root the way Claude Code names its
// transcript folders: every character that is not an ASCII letter, digit or -
// becomes -.
func encodeProjectPath(realPath string) string {
	var b strings.Builder
	for _, r := range realPath {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ShadowRootFor is the shadow root of the project at root (a git repo's main
// root, or a non-git launch folder). Identity is the real path, so a moved
// project gets a new store. Two paths can encode alike (/a-b, /a/b), so a folder
// already held by another project sends this one to <encoded>-<hash8>. A name
// over maxShadowName is cut and gets the same suffix.
func ShadowRootFor(root string) string {
	real := realDir(root)
	sum := sha256.Sum256([]byte(real))
	suffix := "-" + hex.EncodeToString(sum[:4])
	name := encodeProjectPath(real)
	if len(name) > maxShadowName {
		return filepath.Join(ProjectsDir(), name[:maxShadowName-len(suffix)]+suffix)
	}
	primary := filepath.Join(ProjectsDir(), name)
	pf, ok := readProjectFile(primary)
	switch {
	case ok && realDir(pf.CodeRoot) == real:
		return primary
	case ok || isDir(primary): // another project's, or unusable: either way taken
		return primary + suffix
	}
	// The other project may have been removed since this one took the suffix.
	if pf, ok := readProjectFile(primary + suffix); ok && realDir(pf.CodeRoot) == real {
		return primary + suffix
	}
	return primary
}

// CodeRoot is the source repo of the store owner: code_root from a shadow
// root's project.json, or owner itself for an in-repo store.
func CodeRoot(owner string) string {
	if pf, ok := readProjectFile(owner); ok {
		return pf.CodeRoot
	}
	return owner
}

// IsExternalStore reports whether owner is a shadow root.
func IsExternalStore(owner string) bool {
	_, ok := readProjectFile(owner)
	return ok
}

func readProjectFile(owner string) (projectFile, bool) {
	if owner == "" {
		return projectFile{}, false
	}
	data, err := os.ReadFile(filepath.Join(owner, ProjectFileName))
	if err != nil {
		return projectFile{}, false
	}
	var pf projectFile
	if json.Unmarshal(data, &pf) != nil || pf.Kind != externalStoreKind || pf.CodeRoot == "" {
		return projectFile{}, false
	}
	return pf, true
}

// IsStoreOwner reports whether dir holds a project store wipnote may use.
func IsStoreOwner(dir string) bool { return dir != "" && validStoreOwner(dir) }

// IsHomeDir reports whether dir is $HOME, whose .wipnote/ is the application's
// home and not a project store.
func IsHomeDir(dir string) bool {
	home, err := os.UserHomeDir()
	return err == nil && home != "" && realDir(home) == realDir(dir)
}

// IsReservedDir reports a folder that is never a project store owner: $HOME,
// and the app home ~/.wipnote with every folder in it outside a shadow root.
func IsReservedDir(dir string) bool {
	return IsHomeDir(dir) || inAppHome(dir)
}

// InAppHome reports dir at or under the app home ~/.wipnote but not inside a
// shadow root: a folder no store may ever be keyed by.
func InAppHome(dir string) bool { return inAppHome(dir) }

// inAppHome reports dir at or under DataDir but not inside a shadow root.
func inAppHome(dir string) bool {
	rel, err := filepath.Rel(realDir(DataDir()), realDir(dir))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) < 2 || parts[0] != "projects"
}

// hasStoreAt reports a .wipnote at dir that can be a project store.
func hasStoreAt(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".wipnote"))
	return err == nil && !IsReservedDir(dir)
}

// isExplicitStore reports a shadow root made by `wipnote init`.
func isExplicitStore(owner string) bool {
	pf, ok := readProjectFile(owner)
	return ok && pf.Origin == OriginInit
}

// ownerAt is the owner of the store at dir outside a git repo: its .wipnote, or
// the shadow root keyed by dir. Walking up (start false) honours only shadow
// roots made by `wipnote init`, never one a launcher made on its own, and never
// those of $HOME or the filesystem root, which would take in every folder below.
// The app home keys no store at all.
func ownerAt(dir string, start bool) string {
	if hasStoreAt(dir) {
		return dir
	}
	if inAppHome(dir) || (!start && (IsHomeDir(dir) || filepath.Dir(dir) == dir)) {
		return ""
	}
	if shadow := ShadowRootFor(dir); validStoreOwner(shadow) && (start || isExplicitStore(shadow)) {
		return shadow
	}
	return ""
}

// validStoreOwner reports whether dir may serve as a store owner: it holds
// .wipnote/, is not $HOME and, when it is a shadow root, still points at a real checkout. A
// folder under ProjectsDir without a readable project.json is a broken shadow
// root, not an in-repo store.
func validStoreOwner(dir string) bool {
	if !hasStoreAt(dir) {
		return false
	}
	if pf, ok := readProjectFile(dir); ok {
		return isDir(pf.CodeRoot)
	}
	if rel, err := filepath.Rel(realDir(ProjectsDir()), realDir(dir)); err == nil && rel != "." &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// repoScope is the git repo a directory belongs to, for store resolution.
type repoScope struct {
	TopLevel string // the worktree's toplevel, as a real path
	MainRoot string // the main checkout's root, as a real path
}

// scopeOf returns dir's repo. ok is false outside a work tree and when the repo
// is rooted at $HOME: a dotfiles repo at /Users/x must not give every subfolder
// one store, so folders under it (and its linked worktrees) resolve as if no
// repo were there.
func scopeOf(dir string) (repoScope, bool) {
	if dir == "" {
		return repoScope{}, false
	}
	out, err := gitOutput(dir, "rev-parse", "--show-superproject-working-tree", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return repoScope{}, false
	}
	lines := strings.Split(out, "\n")
	if len(lines) == 3 { // a submodule belongs to its superproject's project
		return scopeOf(lines[0])
	}
	if len(lines) != 2 || lines[0] == "" {
		return repoScope{}, false
	}
	top := realDir(lines[0])
	main := mainRootOf(dir, lines[1], top)
	if IsHomeDir(top) || IsHomeDir(main) {
		return repoScope{}, false
	}
	return repoScope{TopLevel: top, MainRoot: main}, true
}

// mainRootOf is the root every worktree of the repo shares. A common dir named
// .git sits in the main checkout, and a bare one (proj/.bare) in the folder
// holding the worktrees, as ResolveViaGitCommonDir has always read both. A
// separate git dir (--separate-git-dir) names the main worktree in git's list.
// Submodules never get here: scopeOf hands them to their superproject.
func mainRootOf(dir, commonDir, top string) string {
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(dir, commonDir)
	}
	commonDir = filepath.Clean(commonDir)
	if filepath.Base(commonDir) == ".git" {
		return realDir(filepath.Dir(commonDir))
	}
	if bare, _ := gitOutput(dir, "--git-dir="+commonDir, "config", "--bool", "core.bare"); bare == "true" {
		return realDir(filepath.Dir(commonDir))
	}
	if list, err := gitOutput(dir, "worktree", "list", "--porcelain"); err == nil {
		if first, ok := strings.CutPrefix(strings.SplitN(list, "\n", 2)[0], "worktree "); ok && first != "" {
			return realDir(first)
		}
	}
	return top
}

// inRepoStore is the nearest folder from start up to the toplevel holding
// .wipnote/, or "". It never looks above the toplevel. maxLevels > 0 limits the
// parents walked, as walkUpForWipnote does.
func (s repoScope) inRepoStore(start string, maxLevels int) string {
	dir, level, found := start, 0, ""
	for {
		if found == "" && hasStoreAt(dir) {
			found = dir
		}
		if realDir(dir) == s.TopLevel {
			return found
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if level++; maxLevels > 0 && level > maxLevels {
			return found
		}
		dir = parent
	}
	// The lexical path never met the toplevel (a symlink inside the repo).
	if real := realDir(start); real != start {
		return s.inRepoStore(real, maxLevels)
	}
	return ""
}

// shadow is the scope's shadow root when it exists and is usable, or "".
func (s repoScope) shadow() string {
	root := ShadowRootFor(s.MainRoot)
	if validStoreOwner(root) {
		return root
	}
	return ""
}

// ExternalStoreFor is the shadow root serving dir, or "" when dir is served by a
// store in its own tree or has no store.
func ExternalStoreFor(dir string) string {
	if dir == "" {
		return ""
	}
	if owner := StoreOwnerFor(dir); IsExternalStore(owner) {
		return owner
	}
	return ""
}

// StoreOwnerFor is the folder whose .wipnote/ serves dir, or "" when there is
// none: in a repo the in-repo store (main root included) or the shadow root,
// never a .wipnote above the toplevel; outside one, the nearest store at or above
// dir, whether in the folder or keyed by it. $HOME never owns one.
func StoreOwnerFor(dir string) string {
	if dir == "" {
		return ""
	}
	if s, ok := scopeOf(dir); ok {
		if owner := s.inRepoStore(dir, 0); owner != "" {
			return owner
		}
		if isDir(filepath.Join(s.MainRoot, ".wipnote")) {
			return s.MainRoot
		}
		return s.shadow()
	}
	if owner := ownerAt(dir, true); owner != "" {
		return owner
	}
	return walkUpForWipnote(dir, 0)
}

// Roots splits dir, which may name either root, into the store owner and the
// source repo. They are the same folder for an in-repo store.
func Roots(dir string) (owner, code string) {
	if IsExternalStore(dir) {
		return dir, CodeRoot(dir)
	}
	if !hasStoreAt(dir) {
		if shadow := ExternalStoreFor(dir); shadow != "" {
			return shadow, dir
		}
	}
	return dir, dir
}

// StoreCreationAllowed reports whether code that needs .wipnote/ under
// projectDir may make it. $HOME's belongs to the application, and inside a repo
// only `wipnote init --in-repo` may; elsewhere the old create-on-demand stands
// for the low-level helpers, which the CLI no longer reaches without a store.
func StoreCreationAllowed(projectDir string) bool {
	if IsReservedDir(projectDir) {
		return false
	}
	if isDir(filepath.Join(projectDir, ".wipnote")) {
		return true
	}
	_, inRepo := scopeOf(projectDir)
	return !inRepo
}

// MainRepoRoot is the main checkout's root for dir, or "" outside a repo (and,
// as in scopeOf, for a repo whose toplevel is $HOME).
func MainRepoRoot(dir string) string {
	s, ok := scopeOf(dir)
	if !ok {
		return ""
	}
	return s.MainRoot
}

// ErrNoStore marks a folder no store serves where wipnote did not make one: the
// app home, a passive caller that never makes stores, or a store that could not
// be made. Commands must not make .wipnote/ on first write, or fall back to a
// .wipnote above the repo; projects get their app-owned store instead.
var ErrNoStore = errors.New("no wipnote store for this project: run `wipnote claude` or `wipnote init` to create one (it is kept under ~/.wipnote/projects, with nothing added here), or `wipnote init --in-repo` to keep it in this folder")

// gitOutput runs git in dir, with the variables that would redirect it to
// another repo removed (a git hook sets them for its own repo).
func gitOutput(dir string, args ...string) (string, error) {
	cmd, cancel := boundedGit(dir, args...)
	defer cancel()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "GIT_DIR" && name != "GIT_WORK_TREE" && name != "GIT_INDEX_FILE" && name != "GIT_COMMON_DIR" {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitProbeTimeout bounds a git probe. Resolution runs in every hook, and a
// directory on a stalled mount (autofs at /home on macOS, a dead network share)
// otherwise blocks git, and the hook, for as long as the mount does.
const gitProbeTimeout = 5 * time.Second

func boundedGit(dir string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), gitProbeTimeout)
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.WaitDelay = time.Second // a git stuck in the kernel cannot hold us past the kill
	return cmd, cancel
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// realDir is p with symlinks resolved, or p cleaned when it does not exist.
func realDir(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}
