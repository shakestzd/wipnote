// Package research holds the research-first gate: the structured research
// record stored on a work item, the change kinds that decide when the gate
// applies, the project config, and the start/dispatch/complete checks. It is
// shared by the CLI (cmd/wipnote) and the PreToolUse hook (core/hooks) so both
// enforce one rule from one definition.
package research

import (
	"encoding/json"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
)

// Property keys on models.Node.Properties. The record is a non-string value, so
// the canonical writer stores it losslessly in the data-node-props JSON escape
// hatch (core/htmlparse/node_props.go); artifacts without it parse unchanged.
const (
	PropRecord = "research"     // the Record, as a JSON object
	PropKind   = "change_kind"  // one of AllKinds
	PropFor    = "research_for" // on a research spike: the item it researches
)

// Source types.
const (
	SourcePrimary   = "primary"
	SourceSecondary = "secondary"
)

// ExistsNoneFound is the literal accepted in Exists when nothing reusable was
// found; it must be accompanied by the Queries that were run.
const ExistsNoneFound = "none found"

// Source is one piece of cited evidence.
type Source struct {
	URL       string `json:"url"`
	Type      string `json:"type,omitempty"`      // primary | secondary
	Retrieved string `json:"retrieved,omitempty"` // YYYY-MM-DD
}

// Rejected is an option considered and turned down, with the reason.
type Rejected struct {
	Option string `json:"option"`
	Reason string `json:"reason"`
}

// Record is the research record. Field order follows MADR 4.0.0
// (problem, considered options, decision outcome, pros and cons).
type Record struct {
	Question string     `json:"question,omitempty"`
	Sources  []Source   `json:"sources,omitempty"`
	Findings string     `json:"findings,omitempty"`
	Exists   string     `json:"exists,omitempty"`  // what already provides this, or "none found"
	Queries  []string   `json:"queries,omitempty"` // searches behind Exists
	Options  []string   `json:"options,omitempty"`
	Chosen   string     `json:"chosen,omitempty"`
	Rejected []Rejected `json:"rejected,omitempty"`
	Gaps     []string   `json:"gaps,omitempty"` // failed sources, unverified claims
	Waiver   string     `json:"waiver,omitempty"`
	SpikeID  string     `json:"spike_id,omitempty"` // spike the record was attached from
}

// legacyEvidenceMarker is the content-note prefix the pre-record dependency
// gate wrote (cmd/wipnote researchEvidenceMarker). Read for old artifacts.
const legacyEvidenceMarker = "research-evidence: "

// FromNode returns the node's research record, or nil when it has none.
// Artifacts written before the record existed are read from their legacy
// research-evidence note so dependency evidence is not lost.
func FromNode(n *models.Node) *Record {
	if n == nil {
		return nil
	}
	if raw, ok := n.Properties[PropRecord]; ok && raw != nil {
		var data []byte
		if s, isStr := raw.(string); isStr {
			data = []byte(s)
		} else {
			data, _ = json.Marshal(raw)
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err == nil {
			return &rec
		}
	}
	return fromLegacyNote(n.Content)
}

func fromLegacyNote(content string) *Record {
	for _, line := range strings.Split(content, "\n") {
		idx := strings.Index(line, legacyEvidenceMarker)
		if idx < 0 {
			continue
		}
		body := stripTags(line[idx+len(legacyEvidenceMarker):])
		rec := &Record{}
		for _, part := range strings.Split(body, " | ") {
			part = strings.TrimSpace(part)
			if w, ok := strings.CutPrefix(part, "waiver: "); ok {
				rec.Waiver = strings.TrimSpace(w)
				continue
			}
			for _, u := range strings.Split(part, ", ") {
				if u = strings.TrimSpace(u); u != "" {
					rec.Sources = append(rec.Sources, Source{URL: u, Type: SourcePrimary})
				}
			}
		}
		return rec
	}
	return nil
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// Property returns the record in the shape stored on Node.Properties.
func (r *Record) Property() map[string]any {
	data, _ := json.Marshal(r)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

// Merge folds src into dst (dst may be nil): non-empty scalars in src win,
// list entries are appended without duplicates.
func Merge(dst, src *Record) *Record {
	out := &Record{}
	if dst != nil {
		*out = *dst
	}
	if src == nil {
		return out
	}
	setIf(&out.Question, src.Question)
	setIf(&out.Findings, src.Findings)
	setIf(&out.Exists, src.Exists)
	setIf(&out.Chosen, src.Chosen)
	setIf(&out.Waiver, src.Waiver)
	setIf(&out.SpikeID, src.SpikeID)
	for _, s := range src.Sources {
		if !hasSource(out.Sources, s.URL) {
			out.Sources = append(out.Sources, s)
		}
	}
	out.Queries = appendUnique(out.Queries, src.Queries...)
	out.Options = appendUnique(out.Options, src.Options...)
	out.Gaps = appendUnique(out.Gaps, src.Gaps...)
	for _, rj := range src.Rejected {
		if !hasRejected(out.Rejected, rj.Option) {
			out.Rejected = append(out.Rejected, rj)
		}
	}
	return out
}

func setIf(dst *string, v string) {
	if v = strings.TrimSpace(v); v != "" {
		*dst = v
	}
}

func hasSource(list []Source, url string) bool {
	for _, s := range list {
		if s.URL == url {
			return true
		}
	}
	return false
}

func hasRejected(list []Rejected, option string) bool {
	for _, r := range list {
		if r.Option == option {
			return true
		}
	}
	return false
}

func appendUnique(list []string, vals ...string) []string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		dup := false
		for _, have := range list {
			if have == v {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, v)
		}
	}
	return list
}

// Assess reports what a record still lacks to satisfy the gate, plus
// non-blocking warnings. A waiver satisfies it outright.
func Assess(r *Record) (missing, warnings []string) {
	if r == nil {
		return []string{"research record"}, nil
	}
	if strings.TrimSpace(r.Waiver) != "" {
		return nil, nil
	}
	if len(r.Sources) == 0 {
		missing = append(missing, "at least one source")
	}
	exists := strings.TrimSpace(r.Exists)
	switch {
	case exists == "":
		missing = append(missing, `"exists" (what already provides this, or "none found" with the queries used)`)
	case strings.EqualFold(exists, ExistsNoneFound) && len(r.Queries) == 0:
		missing = append(missing, `queries behind "exists: none found"`)
	}
	if strings.TrimSpace(r.Chosen) == "" {
		missing = append(missing, "a recorded decision (chosen option)")
	}
	if len(r.Sources) > 0 && allSecondary(r.Sources) {
		warnings = append(warnings, "every source is secondary; cite at least one primary source (official docs, spec, changelog)")
	}
	return missing, warnings
}

func allSecondary(sources []Source) bool {
	for _, s := range sources {
		if s.Type != SourceSecondary {
			return false
		}
	}
	return true
}
