package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
)

// wipResetScope narrows which in-progress items `wip reset` touches.
//
// The three selectors are a UNION: an item is selected when any enabled
// selector matches it. With none enabled every in-progress item is selected
// (the historical all-or-nothing behaviour). The liveness predicate is the
// same one `wip show` uses to print SESSION DEAD? (wipLiveSessions), so the
// cure matches the diagnosis (GH-#176).
type wipResetScope struct {
	// Dead selects items whose latest implemented_in session is NOT open in
	// the session ledger. Items with no session edge at all ("unknown") are
	// deliberately excluded, mirroring `wip show`, which never flags unknown
	// as dead; use Orphaned for those.
	Dead bool
	// Session selects items whose latest implemented_in session equals this id.
	Session string
	// Orphaned selects items with no implemented_in edge ("unknown" owner).
	Orphaned bool
	// DryRun prints the would-reset list and writes nothing.
	DryRun bool
}

// Scoped reports whether any selector narrows the reset.
func (s wipResetScope) Scoped() bool {
	return s.Dead || s.Session != "" || s.Orphaned
}

// Describe returns a short human label for the active selectors, used in
// messages. Empty when unscoped.
func (s wipResetScope) Describe() string {
	var parts []string
	if s.Dead {
		parts = append(parts, "dead sessions")
	}
	if s.Session != "" {
		parts = append(parts, "session "+s.Session)
	}
	if s.Orphaned {
		parts = append(parts, "orphaned (no session)")
	}
	return strings.Join(parts, " + ")
}

// wipResetTarget is one selected item paired with its resolved owner session.
type wipResetTarget struct {
	Node    *models.Node
	Session string // "unknown" when the item has no implemented_in edge
}

// wipResetTargets applies scope to items using the live-session set and
// returns the selected targets in stable order (session ascending, "unknown"
// last, then ID ascending).
func wipResetTargets(items []*models.Node, live map[string]bool, scope wipResetScope) []wipResetTarget {
	bySession := wipGroupBySession(items)
	var out []wipResetTarget
	for _, sess := range wipSortedSessionKeys(bySession) {
		if !wipResetSessionSelected(sess, live, scope) {
			continue
		}
		nodes := bySession[sess]
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
		for _, n := range nodes {
			out = append(out, wipResetTarget{Node: n, Session: sess})
		}
	}
	return out
}

// wipResetSessionSelected is the per-session predicate behind wipResetTargets.
func wipResetSessionSelected(sess string, live map[string]bool, scope wipResetScope) bool {
	if !scope.Scoped() {
		return true
	}
	unknown := sess == "unknown"
	if scope.Orphaned && unknown {
		return true
	}
	if scope.Dead && !unknown && !live[sess] {
		return true
	}
	if scope.Session != "" && sess == scope.Session {
		return true
	}
	return false
}

// printWipResetDryRun writes the would-reset table (ID, TYPE, SESSION, TITLE)
// without touching any file.
func printWipResetDryRun(w io.Writer, targets []wipResetTarget, live map[string]bool) {
	fmt.Fprintf(w, "DRY RUN — %d item(s) would be reset to todo (nothing written)\n\n", len(targets))
	if len(targets) == 0 {
		return
	}
	fmt.Fprintf(w, "%-22s  %-8s  %-16s  %s\n", "ID", "TYPE", "SESSION", "TITLE")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	for _, t := range targets {
		sessDisplay := truncate(t.Session, 16)
		if t.Session != "unknown" && !live[t.Session] {
			sessDisplay = truncate(t.Session, 8) + " [dead?]"
		}
		fmt.Fprintf(w, "%-22s  %-8s  %-16s  %s\n", t.Node.ID, t.Node.Type, sessDisplay, truncate(t.Node.Title, 36))
	}
	fmt.Fprintln(w, "\nRe-run without --dry-run (and with --force) to apply.")
}
