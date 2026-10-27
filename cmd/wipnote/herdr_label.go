package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// labelHerdrPane names the caller's herdr pane "<id>: <title>". Best effort:
// outside herdr nothing runs, and a missing, failing or hung herdr is ignored
// once herdr.LabelTimeout has passed, so a launch or start never fails on it.
func labelHerdrPane(id, title string) {
	env := herdr.EnvFrom(os.Getenv)
	if !env.InHerdr || env.CallerPane == "" || id == "" {
		return
	}
	c := herdr.New(env)
	c.Timeout = herdr.LabelTimeout
	_ = herdr.LabelCallerPane(context.Background(), c, env, herdr.WorkLabel(id, title))
}

// labelHerdrPaneForWorkItem labels the caller's herdr pane for a launch's
// work item, reading its title from projectRoot's .wipnote/. Outside herdr
// or with no work item it reads nothing. An item that cannot be read is
// labelled with its id alone.
func labelHerdrPaneForWorkItem(projectRoot, id string) {
	if id == "" || !herdr.EnvFrom(os.Getenv).InHerdr {
		return
	}
	title := ""
	if projectRoot != "" {
		if n, err := resolveNodeByUnionID(filepath.Join(projectRoot, ".wipnote"), id); err == nil && n != nil {
			id, title = n.ID, n.Title
		}
	}
	labelHerdrPane(id, title)
}

// launchHerdrLabel labels the pane in the background so a launch never waits
// on herdr. The harness runs as a child, so the launcher process outlives the
// call; the returned channel closes when it ends (tests wait on it).
func launchHerdrLabel(projectRoot, id string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		labelHerdrPaneForWorkItem(projectRoot, id)
	}()
	return done
}
