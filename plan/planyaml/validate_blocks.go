package planyaml

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// rawColorRe matches raw CSS colors (hex like #1a2b3c / #abc, or rgb()/rgba()
// /hsl()/hsla() functions). Wireframe blocks must use design tokens (CSS custom
// properties like var(--color-fg)) instead — see validateWireframeBlock.
var rawColorRe = regexp.MustCompile(`(?i)#[0-9a-f]{3,8}\b|\brgba?\(|\bhsla?\(`)

// validateBlock checks one SliceBlock's shape against BlockCatalog. The block's
// type must be a known catalog entry; required scalar fields, row keys, and
// entries are enforced per the spec. Returns a (possibly empty) error slice.
func validateBlock(prefix string, b SliceBlock) []string {
	var errs []string
	spec, ok := blockSpecFor(b.Type)
	if !ok {
		known := make([]string, 0, len(BlockCatalog()))
		for _, s := range BlockCatalog() {
			known = append(known, s.Type)
		}
		return []string{fmt.Sprintf("%s.type %q is unknown; supported: %s", prefix, b.Type, strings.Join(known, "|"))}
	}
	for _, key := range spec.Fields {
		if strings.TrimSpace(b.Fields[key]) == "" {
			errs = append(errs, fmt.Sprintf("%s.fields.%s is required for %s blocks", prefix, key, b.Type))
		}
	}
	errs = append(errs, validateBlockRows(prefix, b, spec)...)
	if spec.RequiresEntries && len(b.Entries) == 0 {
		errs = append(errs, fmt.Sprintf("%s.entries must have at least 1 entry for %s blocks", prefix, b.Type))
	}
	if b.Type == "wireframe" {
		errs = append(errs, validateWireframeBlock(prefix, b)...)
	}
	return errs
}

// validateBlockRows enforces RequiresRows and per-row required keys (RowKeys).
func validateBlockRows(prefix string, b SliceBlock, spec BlockSpec) []string {
	var errs []string
	if spec.RequiresRows && len(b.Rows) == 0 {
		errs = append(errs, fmt.Sprintf("%s.rows must have at least 1 entry for %s blocks", prefix, b.Type))
	}
	if len(spec.RowKeys) == 0 {
		return errs
	}
	for ri, row := range b.Rows {
		for _, key := range spec.RowKeys {
			if strings.TrimSpace(row[key]) == "" {
				errs = append(errs, fmt.Sprintf("%s.rows[%d].%s is required for %s blocks", prefix, ri, key, b.Type))
			}
		}
	}
	return errs
}

// validateWireframeBlock rejects raw CSS colors in a wireframe's html field;
// wireframes must use design tokens (CSS custom properties) so they inherit the
// canonical palette rather than baking in hex/rgb values.
func validateWireframeBlock(prefix string, b SliceBlock) []string {
	if rawColorRe.MatchString(html.UnescapeString(b.Fields["html"])) {
		return []string{fmt.Sprintf("%s.fields.html must use design tokens (var(--...)), not raw hex/rgb/hsl colors", prefix)}
	}
	return nil
}

// blocksWaiverRe matches an explicit, machine-checkable opt-out from the
// blocks gate (see the Validate doc comment): a "blocks_waiver: <reason>"
// marker line inside a slice's decisions_notes. Case-insensitive, tolerant of
// leading whitespace (decisions_notes is often a YAML literal block with
// indentation carried over).
var blocksWaiverRe = regexp.MustCompile(`(?im)^\s*blocks_waiver:\s*(.+?)\s*$`)

// blocksWaiverReason extracts the reason recorded by a "blocks_waiver:"
// marker line in s.DecisionsNotes, or "" if no such marker is present.
//
// This is the skip-reason mechanism for the blocks gate: blocks-first
// planning deliberately allows skipping a visual block when a slice is
// genuinely underspecified (e.g. a pure refactor with no new file, route, or
// schema surface) — forcing block invention in that case would violate the
// grounding rule (never invent file paths, routes, or columns just to fill a
// block). A marker line mirrors the existing research_waiver field's
// contract (an explicit, audited reason, not a silent skip) without adding a
// new PlanSlice field, since decisions_notes is already the free-text
// rationale slot required alongside blocks for the same slices.
func blocksWaiverReason(s PlanSlice) string {
	m := blocksWaiverRe.FindStringSubmatch(s.DecisionsNotes)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}
