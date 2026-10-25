package workitem

import (
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/paths"
)

// nodeTemplateData holds all pre-computed values for the node template.
// Fields that contain trusted HTML use template.HTML to bypass auto-escaping.
type nodeTemplateData struct {
	ID               string
	Title            string
	Type             string
	Status           string
	Priority         string
	CreatedAt        string
	UpdatedAt        string
	AgentAssigned    string
	TrackID          string
	PlanTaskID       string
	SpikeSubtype     string
	ClaimedAt        string
	ClaimedBySession string

	// Provenance — rendered as data-created-by-* attributes on <article>.
	CreatedByAgent      string
	CreatedByModel      string
	CreatedByRole       string
	CreatedByCLIVersion string

	// PropAttrs carries models.Node.Properties as pre-rendered, pre-escaped
	// HTML attributes with a leading space (empty when the node has none).
	// Attribute *names* are dynamic, and html/template cannot compute an
	// output context for an action in attribute-name position, so the whole
	// attribute run is built by nodePropAttrs and injected as HTMLAttr — same
	// approach as edgeData.PropAttrs (defined below) uses for edges.
	PropAttrs template.HTMLAttr

	StatusLabel   string
	PriorityLabel string

	HasEdges   bool
	EdgeGroups []edgeGroupData

	HasSteps bool
	Steps    []stepData

	HasContent     bool
	TrustedContent template.HTML
}

// edgeGroupData holds one group of edges (same relationship type).
type edgeGroupData struct {
	RelType  string
	RelLabel string
	Edges    []edgeData
}

// edgeData holds one edge link for the template.
type edgeData struct {
	TargetID     string
	Href         string
	Relationship string
	Label        string
	HasSince     bool
	Since        string

	// PropAttrs carries models.Edge.Properties as pre-rendered, pre-escaped
	// HTML attributes with a leading space (empty when the edge has none).
	// Attribute *names* are dynamic, and html/template cannot compute an
	// output context for an action in attribute-name position, so the whole
	// attribute run is built by edgePropAttrs and injected as HTMLAttr.
	PropAttrs template.HTMLAttr
}

// renderPropAttrs renders a property map as the attribute run that goes
// inside an element's opening tag, sharing the wire format defined in
// core/htmlparse/edge_props.go (and, for nodes, core/htmlparse/node_props.go).
// isAttrSafe supplies the caller's own key-charset/reserved-name rules (edge
// vs node), and overflowAttr is the JSON escape-hatch attribute name to use
// for anything isAttrSafe rejects.
//
// props is map[string]any rather than map[string]string so this same core
// serves both edge properties (always strings) and node properties (any Go
// value SetProperty was called with). A key only gets the readable
// data-<key> form when BOTH isAttrSafe(key) and the value is a string —
// anything else (attr-unsafe key, or a non-string value that would lose its
// type if flattened to an attribute string) folds into the JSON payload,
// which preserves the value's shape across a parse round-trip.
//
// Injection safety: the returned string is marked HTMLAttr, so it bypasses
// html/template's contextual escaping and must therefore be self-escaping.
// Attribute names are only ever emitted for keys isAttrSafe accepts
// (lowercase [a-z0-9_-], no reserved names — nothing that can close a tag or
// start another attribute), and every value, including the JSON payload,
// goes through template.HTMLEscapeString, which escapes the double quote
// that delimits it.
//
// Keys are sorted, and encoding/json sorts map keys of its own accord, so a
// given property map always renders to the same bytes — a rewrite of an
// unchanged node produces no diff.
func renderPropAttrs(props map[string]any, isAttrSafe func(string) bool, overflowAttr string) template.HTMLAttr {
	if len(props) == 0 {
		return ""
	}

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	overflow := make(map[string]any)
	for _, k := range keys {
		v := props[k]
		if s, ok := v.(string); ok && isAttrSafe(k) {
			fmt.Fprintf(&sb, ` data-%s="%s"`, k, template.HTMLEscapeString(s))
			continue
		}
		overflow[k] = v
	}

	if len(overflow) > 0 {
		if payload, err := json.Marshal(overflow); err == nil {
			fmt.Fprintf(&sb, ` %s="%s"`, overflowAttr,
				template.HTMLEscapeString(string(payload)))
		}
	}

	return template.HTMLAttr(sb.String()) // #nosec G203: escaped above
}

// edgePropAttrs renders an edge's properties as the attribute run that goes
// inside the edge's <a> tag. Edge.Properties is map[string]string (every
// value is already a string), so this is a thin adapter over the shared
// renderPropAttrs core. Returns "" for an edge with no properties.
func edgePropAttrs(props map[string]string) template.HTMLAttr {
	if len(props) == 0 {
		return ""
	}
	anyProps := make(map[string]any, len(props))
	for k, v := range props {
		anyProps[k] = v
	}
	return renderPropAttrs(anyProps, htmlparse.EdgePropKeyIsAttrSafe, htmlparse.EdgePropsAttr)
}

// nodePropAttrs renders a node's properties as the attribute run that goes
// inside <article>, using the wire format defined in
// core/htmlparse/node_props.go. Returns "" for a node with no properties.
func nodePropAttrs(props map[string]any) template.HTMLAttr {
	return renderPropAttrs(props, htmlparse.NodePropKeyIsAttrSafe, htmlparse.NodePropsAttr)
}

// edgeHref returns a collection-aware relative href for a link to targetID.
//
// Work-item HTML lives in per-collection subdirectories of .wipnote/
// (bugs/, features/, spikes/, tracks/, plans/, specs/, sessions/). A bare
// "<id>.html" href only resolves when source and target share a directory;
// cross-collection edges (e.g. a bug's part_of → a track, or implemented_in →
// a session) must hop up one level and into the target's collection
// ("../tracks/trk-….html"). The target collection is derived from the ID
// prefix; session IDs are bare UUIDs with no prefix (fix for bug-fddf5820,
// findings 1).
//
// The parser strips any leading directory component when reading the href back
// (core/htmlparse parser.go), so prefixed hrefs round-trip to the same target
// ID — making this change safe for re-ingest.
func edgeHref(targetID string) string {
	dir := collectionDirForID(targetID)
	if dir == "" {
		return targetID + ".html"
	}
	return "../" + dir + "/" + targetID + ".html"
}

// isSessionID reports whether id looks like a session identifier.
// Two formats are recognised:
//
//  1. RFC 4122 UUID (8-4-4-4-12 hex groups, 36 chars with hyphens), e.g.
//     "127926be-6a1c-4045-a347-e42785ec5839" — produced by Claude Code's own
//     session_id and by uuid.New() fallback in session-start.
//
//  2. OTel compact hex (28 lowercase hex chars, no hyphens), e.g.
//     "019ebc63ba7ae905adb1f8db7504" — produced by generateOtelSessionID()
//     in cmd/wipnote/claude_otel_collect_spawn.go (12-char ms timestamp +
//     16-char entropy). Without this branch, compact IDs fall through
//     collectionDirForID to the default "" case and edgeHref emits a bare
//     filename, breaking the ../sessions/ cross-collection link (bug-91e8aa4c).
//
// Session work items carry no recognised work-item prefix, so they must be
// identified structurally rather than by prefix.
func isSessionID(id string) bool {
	switch len(id) {
	case 36:
		// RFC 4122 UUID: 8-4-4-4-12 hex groups separated by hyphens.
		for i, r := range id {
			switch i {
			case 8, 13, 18, 23:
				if r != '-' {
					return false
				}
			default:
				if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
					return false
				}
			}
		}
		return true
	case 28:
		// OTel compact hex: 28 lowercase hex characters, no separators.
		for _, r := range id {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// stepData holds one implementation step for the template.
type stepData struct {
	CompletedStr string
	StepID       string
	Agent        string
	DependsOnStr string
	Icon         string
	Description  string

	// Provenance — rendered as data-created-by-* attributes on <li>. Agent
	// (above) doubles as the data-created-by-agent value.
	CreatedByModel      string
	CreatedByRole       string
	CreatedByCLIVersion string
}

// newNodeTemplateData converts a models.Node into template-ready data.
// User-supplied text fields (Title, Content, Step descriptions) are sanitized
// through SanitizeHostPaths before rendering so machine-local absolute path
// prefixes never appear in committed work-item HTML (bug-ff6a3286).
func newNodeTemplateData(n *models.Node) *nodeTemplateData {
	d := &nodeTemplateData{
		ID:               n.ID,
		Title:            paths.SanitizeHostPaths(n.Title),
		Type:             n.Type,
		Status:           string(n.Status),
		Priority:         string(n.Priority),
		CreatedAt:        fmtTime(n.CreatedAt),
		UpdatedAt:        fmtTime(n.UpdatedAt),
		AgentAssigned:    n.AgentAssigned,
		TrackID:          n.TrackID,
		PlanTaskID:       n.PlanTaskID,
		SpikeSubtype:     n.SpikeSubtype,
		ClaimedAt:        n.ClaimedAt,
		ClaimedBySession: n.ClaimedBySession,

		CreatedByAgent:      n.CreatedByAgent,
		CreatedByModel:      n.CreatedByModel,
		CreatedByRole:       n.CreatedByRole,
		CreatedByCLIVersion: n.CreatedByCLIVersion,

		PropAttrs: nodePropAttrs(n.Properties),

		StatusLabel:   titleCase(strings.ReplaceAll(string(n.Status), "-", " ")),
		PriorityLabel: titleCase(string(n.Priority)),
	}

	d.EdgeGroups = buildEdgeGroups(n)
	d.HasEdges = len(d.EdgeGroups) > 0

	d.Steps = buildSteps(n.Steps)
	d.HasSteps = len(d.Steps) > 0

	if n.Content != "" {
		d.HasContent = true
		content := paths.SanitizeHostPaths(n.Content)
		// Wrap plain text in <p> so it survives the HTML round-trip.
		// The parser reads element children only, not text nodes.
		//
		// Plain-text descriptions are HTML-escaped first so angle-bracket
		// placeholders (e.g. "<id>", "<path>") are emitted as entities rather
		// than being parsed as tags and silently dropped on re-ingest
		// (bug-fddf5820, finding 2). Content that already begins with "<" is
		// treated as authored HTML and passed through verbatim.
		if !strings.HasPrefix(strings.TrimSpace(content), "<") {
			content = "<p>" + template.HTMLEscapeString(content) + "</p>"
		}
		d.TrustedContent = template.HTML(content) // #nosec: authored HTML
	}

	return d
}

// buildEdgeGroups converts a Node's edges map into template-ready groups.
func buildEdgeGroups(n *models.Node) []edgeGroupData {
	if len(n.Edges) == 0 {
		return nil
	}
	// Relationship types are sorted so a rewrite of an unchanged node emits
	// byte-identical HTML — Go map iteration order would otherwise reshuffle
	// the nav on every write and churn the committed artifact.
	relTypes := make([]string, 0, len(n.Edges))
	for relType := range n.Edges {
		relTypes = append(relTypes, relType)
	}
	sort.Strings(relTypes)

	groups := make([]edgeGroupData, 0, len(n.Edges))
	for _, relType := range relTypes {
		edges := n.Edges[relType]
		if len(edges) == 0 {
			continue
		}
		g := edgeGroupData{
			RelType:  relType,
			RelLabel: titleCase(strings.ReplaceAll(relType, "_", " ")),
			Edges:    make([]edgeData, 0, len(edges)),
		}
		for _, e := range edges {
			label := e.Title
			if label == "" {
				label = e.TargetID
			}
			ed := edgeData{
				TargetID:     e.TargetID,
				Href:         edgeHref(e.TargetID),
				Relationship: string(e.Relationship),
				Label:        label,
				PropAttrs:    edgePropAttrs(e.Properties),
			}
			if !e.Since.IsZero() {
				ed.HasSince = true
				ed.Since = fmtTime(e.Since)
			}
			g.Edges = append(g.Edges, ed)
		}
		groups = append(groups, g)
	}
	return groups
}

// buildSteps converts a slice of model Steps into template-ready data.
func buildSteps(steps []models.Step) []stepData {
	if len(steps) == 0 {
		return nil
	}
	result := make([]stepData, 0, len(steps))
	for _, s := range steps {
		icon := "\u23F3" // hourglass
		completed := "false"
		if s.Completed {
			icon = "\u2705" // checkmark
			completed = "true"
		}
		sd := stepData{
			CompletedStr:        completed,
			StepID:              s.StepID,
			Agent:               s.Agent,
			Icon:                icon,
			Description:         paths.SanitizeHostPaths(s.Description),
			CreatedByModel:      s.CreatedByModel,
			CreatedByRole:       s.CreatedByRole,
			CreatedByCLIVersion: s.CreatedByCLIVersion,
		}
		if len(s.DependsOn) > 0 {
			sd.DependsOnStr = strings.Join(s.DependsOn, ",")
		}
		result = append(result, sd)
	}
	return result
}

// fmtTime formats a time.Time in Python-compatible ISO-8601.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05.999999")
}

// titleCase capitalises the first letter of each word.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
