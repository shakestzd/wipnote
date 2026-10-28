package herdr

// Work-item pane labels: launches and feature start name the caller's herdr
// pane "<id>: <title>" so every herdr view shows why an agent is working.
// The label is best effort: outside herdr nothing runs, and the one call is
// bounded by LabelTimeout so it never holds up a launch.

import (
	"context"
	"strings"
	"time"
)

const (
	// LabelWidth is the most runes a work-item label keeps.
	LabelWidth = 48
	// LabelTimeout bounds the one herdr call that sets a work-item label.
	LabelTimeout = time.Second
)

// WorkLabel is the pane label for a work item: "<id>: <title>" with the
// title's whitespace collapsed, cut to LabelWidth runes (the last one an
// ellipsis when cut). An empty title gives the id alone.
func WorkLabel(id, title string) string {
	label := strings.TrimSpace(id)
	if t := strings.Join(strings.Fields(title), " "); t != "" {
		label += ": " + t
	}
	if rs := []rune(label); len(rs) > LabelWidth {
		label = string(rs[:LabelWidth-1]) + "…"
	}
	return label
}

// LabelCallerPane sets the caller's pane label through c. Outside herdr,
// with no caller pane or with a blank label it runs nothing and returns nil.
// Callers that must not fail on it ignore the error.
func LabelCallerPane(ctx context.Context, c Client, env Env, label string) error {
	if !env.InHerdr || env.CallerPane == "" || strings.TrimSpace(label) == "" {
		return nil
	}
	return c.Rename(ctx, env.CallerPane, label)
}
