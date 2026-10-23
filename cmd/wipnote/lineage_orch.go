package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shakestzd/wipnote/internal/orchlink"
)

// lineageOrchView is the orch section of a work item's lineage: the orch
// sessions that serve it (docs/design/orch-integration.md). Err is set when
// the orch registry exists but cannot be read, so that case never reads as
// "no orch sessions".
type lineageOrchView struct {
	Sessions []orchlink.Session
	Err      string
}

// isWorkItemLineageKind reports whether an orch session can serve the root.
func isWorkItemLineageKind(kind lineageKind) bool {
	switch kind {
	case kindFeature, kindBug, kindSpike, kindPlan, kindTrack:
		return true
	default:
		return false
	}
}

// loadLineageOrch reads the orch registry for the sessions serving root. It
// returns nil when the root is not a work item or nothing serves it.
func loadLineageOrch(root string, kind lineageKind, forward []lineageNode) *lineageOrchView {
	if !isWorkItemLineageKind(kind) {
		return nil
	}
	home, _ := os.UserHomeDir()
	path := orchlink.RegistryPath(os.Getenv, home)
	sessions, err := orchlink.ForWorkItem(path, root, directSessions(root, forward))
	if err != nil {
		return &lineageOrchView{Err: err.Error()}
	}
	if len(sessions) == 0 {
		return nil
	}
	return &lineageOrchView{Sessions: sessions}
}

// directSessions returns the ids of the sessions the root was implemented in
// (its own implemented_in edges, not those of related items).
func directSessions(root string, forward []lineageNode) map[string]bool {
	out := map[string]bool{}
	for _, n := range forward {
		if n.Parent == root && n.EdgeType == "implemented_in" {
			out[n.ID] = true
		}
	}
	return out
}

// printLineageOrch renders the orch section below the tree.
func printLineageOrch(w io.Writer, v *lineageOrchView) {
	if v == nil {
		return
	}
	if v.Err != "" {
		fmt.Fprintf(w, "\n  orch sessions: unreadable (%s)\n", v.Err)
		return
	}
	fmt.Fprintf(w, "\n  orch sessions (%d):\n", len(v.Sessions))
	for _, s := range v.Sessions {
		where := "host"
		if s.Container {
			where = "container"
		}
		link := "tagged"
		if s.Link == orchlink.LinkSID {
			link = "derived: session id"
		}
		fmt.Fprintf(w, "    %s  [%s, %s, %s]  sub-agents %d  (%s)\n", s.Name, s.Kind, s.State, where, s.Subagents, link)
		if len(s.SessionIDs) > 0 {
			fmt.Fprintf(w, "      sessions: %s\n", strings.Join(s.SessionIDs, ", "))
		}
	}
}
