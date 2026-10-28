package oneshot

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/container"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Container runs (plan-28b4a7f6 slice 21, decision C): with --container the
// tool runs inside the project's own devcontainer. The caller refuses
// before anything is written when the project has no devcontainer config or
// the CLI is missing; the run itself (in its pane, or headless) runs
// devcontainer up, then the tool through devcontainer exec, with the run
// folder's paths mapped into the container's workspace folder. The tool's
// output comes back through exec, so the logs are written on the host.

// Run-folder files of a container run.
const (
	FileContainer = runstore.FileContainer // where it ran: place and workspace folder
	FileUpLog     = runstore.FileUpLog     // devcontainer up's progress
)

// ContainerJob is a job's container part: the project root, the
// --workspace-folder, and the env set inside.
type ContainerJob struct {
	Root string   `json:"root"`
	Env  []string `json:"env"`
}

type containerRecord struct {
	Place           string `json:"place"`
	WorkspaceFolder string `json:"workspace_folder"`
	ContainerID     string `json:"container_id,omitempty"`
	RemoteUser      string `json:"remote_user,omitempty"`
}

// ClassifyContainer reads a --container argv. claude -p and codex argvs are
// classified as without it. Any other argv is a ccx task's arguments (the
// redirect's `--container -- <task args>`): ccx is container Claude, so
// they run as claude -p. ccx task itself is refused: --container already
// is the container.
func ClassifyContainer(argv []string) (Recipe, error) {
	if len(argv) == 0 {
		return Recipe{}, fmt.Errorf("delegate run: no command after --")
	}
	switch filepath.Base(argv[0]) {
	case "claude", "codex":
		return Classify(argv)
	case "ccx":
		return Recipe{}, fmt.Errorf("delegate run: --container runs Claude in the project's devcontainer itself; pass the task's arguments (--container -- <task args>), not ccx")
	}
	r, err := Classify(append([]string{"claude", "-p"}, argv...))
	if err != nil {
		return Recipe{}, fmt.Errorf("%w (--container read %q as a task for claude -p)", err, strings.Join(argv, " "))
	}
	return r, nil
}

func classify(o Options) (Recipe, error) {
	if o.Container {
		return ClassifyContainer(o.Argv)
	}
	return Classify(o.Argv)
}

// insideProbe is what the ladder is told about bwrap in a container.
const insideProbe = "bwrap cannot create its namespaces inside the devcontainer, so Codex's sandbox does not work there (--container)"

// containerCodex is slice 12's ladder for Codex inside the container, where
// bwrap fails: refused unless this host opted in, then danger-full-access.
// codex review takes no --sandbox, so it is refused there.
func containerCodex(o Options, r Recipe) (codex.ProbeResult, error) {
	if r.Kind == KindCodexReview {
		return codex.ProbeResult{}, fmt.Errorf("delegate run: %w: codex review cannot be given a --sandbox, and bwrap does not work inside the devcontainer; run codex review without --container", codex.ErrRefused)
	}
	return codex.ProbeResult{Status: codex.ProbeFail, Output: insideProbe}, nil
}

// enterContainer runs devcontainer up and turns job into the same job
// inside: argv and cwd mapped, run through devcontainer exec.
func enterContainer(ctx context.Context, rf *runstore.RunFolder, job Job, l *runLogs) (Job, error) {
	c := job.Container
	l.note("starting the project's devcontainer (devcontainer up); progress in " + FileUpLog)
	progress, err := appendFile(rf, FileUpLog)
	if err != nil {
		return job, err
	}
	up, err := container.RunUp(ctx, c.Root, progress)
	progress.Close()
	if err != nil {
		return job, err
	}
	rec, _ := json.MarshalIndent(containerRecord{Place: "container", WorkspaceFolder: up.RemoteWorkspaceFolder,
		ContainerID: up.ContainerID, RemoteUser: up.RemoteUser}, "", "  ")
	_ = rf.WriteFile(FileContainer, append(rec, '\n'))
	m := container.Mapper{Root: c.Root, Remote: up.RemoteWorkspaceFolder}
	cwd, err := m.Path(job.Cwd)
	if err != nil {
		return job, err
	}
	inside := job
	inside.Argv = container.ExecArgv(c.Root, c.Env, container.InDir(cwd, up.RemoteWorkspaceFolder, m.Argv(job.Argv, filepath.Join(rf.Dir(), runstore.FileLastMD))))
	return inside, nil
}

// executeContainer enters the container, or ends the run with exit 1 and
// the reason in its logs.
func executeContainer(ctx context.Context, rf *runstore.RunFolder, job Job) (Job, bool) {
	l, err := openLogs(rf)
	if err != nil {
		return job, false
	}
	defer l.close()
	inside, err := enterContainer(ctx, rf, job, l)
	if err != nil {
		msg := fmt.Sprintf("delegate run --container: %v", err)
		_, _ = l.raw.WriteString(msg + "\n")
		l.note(msg)
		return job, false
	}
	return inside, true
}

func checkContainer(o Options, r Recipe) error {
	if !o.Container {
		return nil
	}
	if o.Root == "" {
		return fmt.Errorf("delegate run: --container needs the project root")
	}
	if err := container.Check(o.Root); err != nil {
		return err
	}
	if _, err := (container.Mapper{Root: o.Root, Remote: "/"}).Path(o.Cwd); err != nil {
		return fmt.Errorf("delegate run --container: the working folder must be inside the project: %w", err)
	}
	if own, ok := r.OwnLastFile(); ok && own != "" {
		if !filepath.IsAbs(own) {
			own = filepath.Join(o.Cwd, own)
		}
		if _, err := (container.Mapper{Root: o.Root, Remote: "/"}).Path(own); err != nil {
			// The host would read its own file there, not the tool's answer.
			return fmt.Errorf("delegate run --container: codex's -o file is outside the project, so it is not in the container: %w", err)
		}
	}
	return nil
}
