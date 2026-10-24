package hooks

import (
	"encoding/json"
	"regexp"
	"strings"
)

// sessionEndFilePointerLimit is how many recently-touched files the session-end
// handoff records.
const sessionEndFilePointerLimit = 5

// fileEditTools are the tools whose canonical tool-event records count as a
// file touch for the session-end handoff.
var fileEditTools = map[string]bool{
	"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true,
}

// filePathInputKeys are the tool_input keys that carry a single target path.
// NotebookEdit names it notebook_path.
var filePathInputKeys = []string{"file_path", "path", "notebook_path"}

// quotedJSONFieldRe pulls a complete string value for key out of a JSON object
// that may have been cut off elsewhere (canonical fields are truncated at
// canonicalToolFieldMax bytes).
func quotedJSONFieldRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*("(?:[^"\\]|\\.)*")`)
}

var filePathFieldRes = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(filePathInputKeys))
	for i, k := range filePathInputKeys {
		out[i] = quotedJSONFieldRe(k)
	}
	return out
}()

// canonicalEventTouchedPaths returns the file paths one canonical tool-event
// record touched, most significant first. It tolerates truncated input JSON.
func canonicalEventTouchedPaths(ev canonicalToolEvent) []string {
	if !fileEditTools[ev.Tool] {
		return nil
	}
	var input map[string]any
	parsed := ev.Input != "" && json.Unmarshal([]byte(ev.Input), &input) == nil

	if ev.Tool == "apply_patch" {
		if parsed {
			patch, _ := input["patch"].(string)
			if patch == "" {
				patch, _ = input["input"].(string)
			}
			return applyPatchTouchedPaths(patch)
		}
		return truncatedApplyPatchPaths(ev.Input)
	}

	if parsed {
		for _, k := range filePathInputKeys {
			if v, ok := input[k].(string); ok && strings.TrimSpace(v) != "" {
				return []string{v}
			}
		}
		return nil
	}
	// Truncated JSON: recover a fully-present path field by regex.
	for _, re := range filePathFieldRes {
		if m := re.FindStringSubmatch(ev.Input); len(m) == 2 {
			var v string
			if json.Unmarshal([]byte(m[1]), &v) == nil && strings.TrimSpace(v) != "" {
				return []string{v}
			}
		}
	}
	// Last resort: the summary is the path (SummariseInput) unless it was
	// shortened, which it marks with a trailing ellipsis.
	if s := strings.TrimSpace(ev.Summary); s != "" && !strings.HasSuffix(s, "…") && !strings.ContainsAny(s, "{}\n") {
		return []string{s}
	}
	return nil
}

// truncatedApplyPatchPaths recovers apply_patch headers from a truncated JSON
// encoding of the input, where newlines are still escaped. A header on the last
// (cut-off) line is dropped because its path may be incomplete.
func truncatedApplyPatchPaths(raw string) []string {
	text := strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`).Replace(raw)
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		text = text[:i+1]
	} else {
		return nil
	}
	return applyPatchTouchedPaths(text)
}

// recentCanonicalFilePointers derives the last sessionEndFilePointerLimit
// distinct file paths the session touched, newest first, from the canonical
// tool-events.ndjson log. normalize maps each raw path to its handoff form
// ("" drops it); dedupe is on the normalized form.
func recentCanonicalFilePointers(projectDir, sessionID string, normalize func(string) string) []string {
	events, _ := readCanonicalToolEvents(projectDir, sessionID)
	// Edits made after the log hit its cap live in the overflow ledger; they
	// are the newest, so they go last and are read first.
	events = append(events, readRecentEditOverflow(recentEditOverflowPath(canonicalToolLogPath(projectDir, sessionID)))...)
	if len(events) == 0 {
		return []string{}
	}
	seen := make(map[string]bool)
	out := make([]string, 0, sessionEndFilePointerLimit)
	for i := len(events) - 1; i >= 0 && len(out) < sessionEndFilePointerLimit; i-- {
		for _, p := range canonicalEventTouchedPaths(events[i]) {
			fp := normalize(p)
			if fp == "" || seen[fp] {
				continue
			}
			seen[fp] = true
			out = append(out, fp)
			if len(out) == sessionEndFilePointerLimit {
				break
			}
		}
	}
	return out
}
