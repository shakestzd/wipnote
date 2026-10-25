package oneshot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/render"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// paneRegistryDir holds, per caller pane, the caller's live one-shot panes
// in creation order (the mod's $TMPDIR/codex-herdr-panes.<pane>), under the
// run store so only this user writes there.
const paneRegistryDir = "panes"

// shq quotes one word for a POSIX shell: $, backticks and quotes stay literal.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// PaneLine is the one shell line herdr types into a run's pane: the wipnote
// binary, its subcommand words, the project and the run id, each quoted.
// The tool's argv is not on the line; the pane worker reads it from job.json.
func PaneLine(exe string, sub []string, projectDir, runID string) string {
	words := append([]string{exe}, sub...)
	words = append(words, "--project-dir", projectDir, runID)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = shq(w)
	}
	return strings.Join(quoted, " ")
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func registryPath(store *runstore.Store, caller herdr.PaneID) string {
	name := unsafeName.ReplaceAllString(string(caller), "_")
	if name == "" {
		name = "cur"
	}
	return filepath.Join(store.Root(), paneRegistryDir, name)
}

func readRegistry(path string) []herdr.PaneID {
	data, _ := os.ReadFile(path)
	var out []herdr.PaneID
	for _, l := range strings.Fields(string(data)) {
		out = append(out, herdr.PaneID(l))
	}
	return out
}

func writeRegistry(path string, live []herdr.PaneID) {
	var b strings.Builder
	for _, p := range live {
		b.WriteString(string(p) + "\n")
	}
	_ = filelock.WriteFileAtomic(path, []byte(b.String()), 0o600)
}

// placePane gives the run a pane. It returns no pane, and maybe a note to
// print, when the run must go headless: the tile cap, or a split herdr
// refused. Fixed layouts split the caller's pane and keep no registry.
func placePane(ctx context.Context, o Options) (pane herdr.PaneID, registry, note string) {
	if fixed, dir := o.Settings.Placement(); fixed {
		p, err := o.Herdr.Split(ctx, "", dir, o.Cwd)
		if err != nil {
			return "", "", ""
		}
		return p, "", ""
	}
	registry = registryPath(o.Store, o.HerdrEnv.CallerPane)
	if err := os.MkdirAll(filepath.Dir(registry), 0o700); err != nil {
		return "", "", ""
	}
	release := filelock.Guard(registry)
	defer release()
	live := pruneLive(ctx, o.Herdr, readRegistry(registry))
	target, dir, ok := herdr.TileTarget(live, o.Settings.MaxPanes)
	if !ok {
		writeRegistry(registry, live)
		return "", "", fmt.Sprintf("herdr pane limit reached (%d delegation panes open, layout limit %d): ran without a pane",
			len(live), max(1, o.Settings.MaxPanes))
	}
	p, err := o.Herdr.Split(ctx, target, dir, o.Cwd)
	if err == nil {
		live = append(live, p)
	}
	writeRegistry(registry, live)
	if err != nil {
		return "", "", ""
	}
	return p, registry, ""
}

// pruneLive keeps the panes herdr does not say are gone; any other lookup
// failure keeps the pane, so a flaky query never frees a place.
func pruneLive(ctx context.Context, c herdr.Client, panes []herdr.PaneID) []herdr.PaneID {
	var live []herdr.PaneID
	for _, p := range panes {
		if _, err := c.Get(ctx, p); !herdr.IsAbsent(err) {
			live = append(live, p)
		}
	}
	return live
}

// unregister drops a pane from its tile registry (the mod's __unreg).
func unregister(registry string, pane herdr.PaneID) {
	if registry == "" {
		return
	}
	release := filelock.Guard(registry)
	defer release()
	var keep []herdr.PaneID
	for _, p := range readRegistry(registry) {
		if p != pane {
			keep = append(keep, p)
		}
	}
	writeRegistry(registry, keep)
}

// runInPane hands the job to a pane worker, names the pane after it runs
// (so the shell's own title for the typed line does not replace it) and
// waits for the exit file. A pane herdr cannot report any more was closed
// early: exit 130, end reason pane_closed. ok is false when herdr would not
// run the line; the caller then runs the job headless.
func runInPane(ctx context.Context, o Options, rf *runstore.RunFolder, r Recipe, pane herdr.PaneID) (exit int, closed, ok bool, err error) {
	if err := o.Herdr.Run(ctx, pane, o.PaneLine(rf.ID())); err != nil {
		return 0, false, false, nil
	}
	info, _ := rf.Read()
	_ = o.Herdr.Rename(ctx, pane, info.Label)
	for {
		if info, err := rf.Read(); err == nil && info.Finished {
			return info.Exit, false, true, nil
		}
		if _, err := o.Herdr.Get(ctx, pane); err != nil {
			fmt.Fprintf(o.Stdout, "the herdr pane %s was closed before %s finished (log: %s)\n", pane, r.Name(),
				filepath.Join(rf.Dir(), runstore.FileRawLog))
			_ = rf.Finish(130, time.Now())
			return 130, true, true, nil
		}
		select {
		case <-ctx.Done():
			return 0, false, true, fmt.Errorf("delegate run: interrupted; the run goes on in herdr pane %s (run folder %s)", pane, rf.Dir())
		case <-time.After(o.poll()):
		}
	}
}

// PaneOptions are the pane worker's (wipnote delegate run-pane).
type PaneOptions struct {
	Store  *runstore.Store
	RunID  string
	Herdr  herdr.Client // for closing its own pane
	Out    io.Writer    // the pane's terminal
	Poll   time.Duration
	Ledger *delegationledger.Store
}

// RunPane is the pane worker: it sets the pane's title and clears the typed
// line, runs the job while the Go viewer follows the run folder, closes the
// run's ledger row when the tool exits (so a caller that was interrupted
// still leaves a closed row), then closes its pane per closePane after
// closeDelay. It returns the tool's exit.
func RunPane(ctx context.Context, po PaneOptions) int {
	rf, err := po.Store.Run(po.RunID)
	if err != nil {
		fmt.Fprintln(po.Out, err)
		return 2
	}
	job, err := readJob(rf)
	if err != nil {
		fmt.Fprintln(po.Out, err)
		return 2
	}
	info, _ := rf.Read()
	fmt.Fprintf(po.Out, "\033]0;%s\007\033[H\033[2J", info.Label)
	done := make(chan int, 1)
	go func() {
		exit := Execute(ctx, rf, job)
		closeOwnRow(ctx, po.Ledger, rf, exit)
		done <- exit
	}()
	_ = render.View(ctx, rf, po.Out, render.ViewOptions{Poll: po.Poll})
	exit := <-done
	closeOwnPane(ctx, po.Herdr, job, exit)
	return exit
}

// closeOwnRow closes the run's row with the tool's exit and usage when the
// row is still open; a row already closed (a stop, or the caller first) is
// left as it is (rule F-1). A cancelled worker records a stop, as the
// caller does.
func closeOwnRow(ctx context.Context, l *delegationledger.Store, rf *runstore.RunFolder, exit int) {
	if l == nil {
		return
	}
	reason := delegationledger.EndExit
	if ctx.Err() != nil {
		reason = delegationledger.EndStop
	}
	c := delegationledger.CloseInfo{Ended: time.Now().UTC(), Exit: &exit, Reason: reason}
	if closed, err := l.CloseIfOpen(rf.ID(), c); err == nil && closed {
		enrichRow(l, rf, rf.ID())
	}
}

func closeOwnPane(ctx context.Context, c herdr.Client, job Job, exit int) {
	if job.Pane == "" || c == nil {
		return
	}
	if job.ClosePane != "always" && (job.ClosePane != "success" || exit != 0) {
		return
	}
	time.Sleep(time.Duration(max(0, job.CloseDelay)) * time.Second)
	unregister(job.Registry, herdr.PaneID(job.Pane))
	_ = c.Close(ctx, herdr.PaneID(job.Pane))
}
