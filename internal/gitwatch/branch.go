package gitwatch

// Branch health for the checked-out branch: ahead/behind a base, dirty and
// unpushed counts, and its sync state. Local git only, through Runner.
//
// Ported in part (MIT; see THIRD-PARTY-NOTICES.md):
//   - git-town internal/git/commands.go (branchesQuery's NUL-separated
//     for-each-ref format, determineSyncStatus) and
//     internal/git/gitdomain/sync_status.go (the SyncStatus values), commit
//     d0cc7951b01e74ba36ba057c0f31fd5eee6e83b8. Copyright (c) 2014-2024 the
//     Git Town creators. Adapted: no field-name prefixes, a committer date
//     column, and an unknown track is SyncUnknown instead of a panic.
//   - lazygit pkg/commands/git_commands/ahead_behind.go
//     (parseAheadBehindField, the %(ahead-behind:<base>) field), commit
//     4e1eebe278790f257a875da6633234391a5a85b3. Copyright (c) 2018 Jesse
//     Duffield. Adapted to one base and without the lo helpers.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SyncStatus is where a branch stands against its upstream (git-town's enum,
// less the states that need remote-only branches).
type SyncStatus string

// Sync states, as git-town names them.
const (
	SyncAhead           SyncStatus = "ahead"             // local has commits the upstream lacks
	SyncBehind          SyncStatus = "behind"            // upstream has commits local lacks
	SyncDeletedAtRemote SyncStatus = "deleted at remote" // the upstream branch is gone
	SyncLocalOnly       SyncStatus = "local only"        // never pushed: no upstream
	SyncNotInSync       SyncStatus = "not in sync"       // both ahead and behind
	SyncUpToDate        SyncStatus = "up to date"
	SyncUnknown         SyncStatus = "unknown" // a track text git-town does not know
)

// Branch is the local health of the checked-out branch. Base is the ref
// ahead/behind count against (origin/<base> when it exists); it is empty,
// and Ahead and Behind zero, when that base could not be found. Unpushed is
// ahead of the upstream, or with no upstream the commits on no remote.
type Branch struct {
	Name     string     `json:"name"`
	Base     string     `json:"base"`
	Ahead    int        `json:"ahead"`
	Behind   int        `json:"behind"`
	Dirty    int        `json:"dirty"`
	Unpushed int        `json:"unpushed"`
	Upstream string     `json:"upstream,omitempty"`
	Sync     SyncStatus `json:"sync"`
}

// ReadBranch reads the checked-out branch in dir ("" is the working
// directory) against base ("" is main). Only an unreadable git status is an
// error; a missing base leaves Base empty.
func ReadBranch(ctx context.Context, r Runner, dir, base string) (*Branch, error) {
	out, err := gitOut(ctx, r, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil, err
	}
	st := parseStatusV2(out)
	b := &Branch{Name: st.head, Upstream: st.upstream, Dirty: st.dirty,
		Sync: determineSyncStatus(st.track(), st.upstream)}
	if st.hasAB {
		b.Unpushed = st.ahead
	} else if n, err := gitOut(ctx, r, dir, "rev-list", "--count", "HEAD", "--not", "--remotes"); err == nil {
		b.Unpushed, _ = strconv.Atoi(strings.TrimSpace(n))
	}
	if ref, err := ResolveBase(ctx, r, dir, base); err == nil {
		if ahead, behind, err := AheadBehind(ctx, r, dir, ref); err == nil {
			b.Base, b.Ahead, b.Behind = ref, ahead, behind
		}
	}
	return b, nil
}

// ResolveBase turns a base branch name ("" is main) into the ref to compare
// against: origin/<base> when that remote branch exists, else base itself
// when it names a commit.
func ResolveBase(ctx context.Context, r Runner, dir, base string) (string, error) {
	if base == "" {
		base = "main"
	}
	if _, err := gitOut(ctx, r, dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+base); err == nil {
		return "origin/" + base, nil
	}
	if _, err := gitOut(ctx, r, dir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err == nil {
		return base, nil
	}
	return "", fmt.Errorf("base %s not found", base)
}

// AheadBehind counts the commits HEAD has that base lacks (ahead) and the
// reverse (behind). It reads %(ahead-behind:<base>) (git 2.41+) and falls
// back to rev-list --left-right on older git or a detached HEAD.
func AheadBehind(ctx context.Context, r Runner, dir, base string) (ahead, behind int, err error) {
	if rows, abOK, err := readRefs(ctx, r, dir, base); err == nil && abOK {
		for _, row := range rows {
			if row.head && row.ab.valid {
				return row.ab.ahead, row.ab.behind, nil
			}
		}
	}
	out, err := gitOut(ctx, r, dir, "rev-list", "--left-right", "--count", base+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %q", out)
	}
	behind, _ = strconv.Atoi(parts[0])
	ahead, _ = strconv.Atoi(parts[1])
	return ahead, behind, nil
}

// refRow is one local branch from readRefs.
type refRow struct {
	name, sha, worktree, upstream, track string
	head                                 bool
	committed                            int64 // committer date, unix seconds
	ab                                   aheadBehind
}

// refFormat is git-town's branchesQuery format (NUL-separated), plus the
// committer date and, when base is set, lazygit's ahead-behind field.
func refFormat(base string) string {
	f := []string{
		"%(refname:lstrip=2)",                // branch name
		"%(objectname)",                      // tip sha
		"%(if)%(HEAD)%(then)Y%(else)N%(end)", // checked out here?
		"%(worktreepath)",                    // worktree holding it, if any
		"%(upstream:lstrip=2)",               // e.g. origin/feat
		"%(upstream:track,nobracket)",        // "ahead 2, behind 3", "gone"
		"%(committerdate:unix)",              // for idle
	}
	if base != "" {
		f = append(f, "%(ahead-behind:"+base+")") // "<ahead> <behind>", git 2.41+
	}
	return "--format=" + strings.Join(f, "%00")
}

// readRefs lists local branches in one for-each-ref call. With a base it
// asks for ahead-behind too; when git rejects that (older than 2.41) it
// retries without it and abOK is false.
func readRefs(ctx context.Context, r Runner, dir, base string) (rows []refRow, abOK bool, err error) {
	out, err := gitOut(ctx, r, dir, "for-each-ref", refFormat(base), "refs/heads/")
	abOK = base != ""
	if err != nil && base != "" {
		abOK = false
		out, err = gitOut(ctx, r, dir, "for-each-ref", refFormat(""), "refs/heads/")
	}
	if err != nil {
		return nil, false, err
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if row, ok := parseRefRow(line, abOK); ok {
			rows = append(rows, row)
		}
	}
	return rows, abOK, nil
}

func parseRefRow(line string, withAB bool) (refRow, bool) {
	want := 7
	if withAB {
		want = 8
	}
	cols := strings.Split(line, "\x00")
	if len(cols) != want {
		return refRow{}, false
	}
	row := refRow{name: cols[0], sha: cols[1], head: cols[2] == "Y", worktree: cols[3],
		upstream: cols[4], track: cols[5]}
	row.committed, _ = strconv.ParseInt(cols[6], 10, 64)
	if withAB {
		row.ab = parseAheadBehindField(cols[7])
	}
	return row, true
}

// determineSyncStatus is git-town's: track is %(upstream:track,nobracket).
func determineSyncStatus(track, upstream string) SyncStatus {
	gone := track == "gone"
	ahead := strings.Contains(track, "ahead")
	behind := strings.Contains(track, "behind")
	switch {
	case gone:
		return SyncDeletedAtRemote
	case ahead && behind:
		return SyncNotInSync
	case ahead:
		return SyncAhead
	case behind:
		return SyncBehind
	case track == "":
		if upstream != "" {
			return SyncUpToDate
		}
		return SyncLocalOnly
	default:
		return SyncUnknown
	}
}

// aheadBehind is one parsed %(ahead-behind:<base>) field.
type aheadBehind struct {
	ahead, behind int
	valid         bool
}

// parseAheadBehindField is lazygit's: "<ahead> <behind>", else invalid.
func parseAheadBehindField(s string) aheadBehind {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return aheadBehind{}
	}
	ahead, err1 := strconv.Atoi(parts[0])
	behind, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return aheadBehind{}
	}
	return aheadBehind{ahead: ahead, behind: behind, valid: true}
}

// statusV2 is what ReadBranch takes from git status --porcelain=v2 --branch.
type statusV2 struct {
	head, upstream       string
	ahead, behind, dirty int
	hasAB                bool // a "# branch.ab" line: the upstream exists
}

func parseStatusV2(out string) statusV2 {
	var st statusV2
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "# branch.head "):
			st.head = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.upstream "):
			st.upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			_, err := fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &st.ahead, &st.behind)
			st.hasAB = err == nil
		case strings.HasPrefix(line, "#"):
		default:
			st.dirty++
		}
	}
	return st
}

// track renders the status as %(upstream:track,nobracket) would: an
// upstream with no ab line is gone.
func (s statusV2) track() string {
	switch {
	case s.upstream == "":
		return ""
	case !s.hasAB:
		return "gone"
	case s.ahead > 0 && s.behind > 0:
		return fmt.Sprintf("ahead %d, behind %d", s.ahead, s.behind)
	case s.ahead > 0:
		return fmt.Sprintf("ahead %d", s.ahead)
	case s.behind > 0:
		return fmt.Sprintf("behind %d", s.behind)
	}
	return ""
}

// gitOut runs git (in dir when set) and returns stdout. A non-zero exit is
// an error carrying the first line of stderr.
func gitOut(ctx context.Context, r Runner, dir string, args ...string) (string, error) {
	out, stderr, code, err := gitRun(ctx, r, dir, args...)
	if err == nil && code != 0 {
		msg := firstLine([]byte(stderr))
		if msg == "" {
			msg = fmt.Sprintf("exit %d", code)
		}
		err = fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out, err
}

// gitRun runs git (in dir when set). err is only "could not run git"; a
// non-zero exit comes back as code.
func gitRun(ctx context.Context, r Runner, dir string, args ...string) (stdout, stderr string, code int, err error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	out, errOut, code, err := r.Run(ctx, "git", args...)
	return string(out), string(errOut), code, err
}
