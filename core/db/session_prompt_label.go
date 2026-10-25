package db

import (
	"database/sql"
	"regexp"
	"strings"
)

// Injected-markup vocabulary, split by stripping strategy so ordinary user
// prompt text — including JSX/XML/HTML such as <Button> or <summary> — is
// preserved, and ONLY harness-injected metadata is removed (roborev jobs 446, 449).

// reWrapperInjectedBlock strips a harness-injected WRAPPER block and its entire
// content. These wrappers contain nested metadata (<status>/<summary>/<note>/
// <result>/<task-id>…), so removing the whole <tag>…</tag> also removes the
// nested generic-named tags WITHOUT stripping those generic names globally
// (which would mangle real prompts like "<summary>View Plan YAML</summary>").
// RE2 has no backreferences, so each alternative closes on its OWN tag — that is
// what makes a nested </task-id> not terminate a <task-notification> match early.
var reWrapperInjectedBlock = regexp.MustCompile(
	`(?s)<task-notification\b[^>]*>.*?</task-notification\s*>` +
		`|<system-reminder\b[^>]*>.*?</system-reminder\s*>` +
		`|<tool-use-error\b[^>]*>.*?</tool-use-error\s*>`)

// standaloneInjectedTagAlt are unambiguous injected tags (ids, file paths) whose
// paired content is pure noise. Deliberately EXCLUDES status/summary/note/result:
// those are common HTML tag names and are injected only INSIDE a wrapper block
// (handled above), so they must not be stripped globally.
const standaloneInjectedTagAlt = `task-id|tool-use-id|output-file`

// rePairedInjectedTag strips a standalone injected paired tag + its content,
// e.g. <task-id>abc123</task-id>, for fragments outside any wrapper.
var rePairedInjectedTag = regexp.MustCompile(`(?s)<(?:` + standaloneInjectedTagAlt + `)\b[^>]*>.*?</(?:` + standaloneInjectedTagAlt + `)\s*>`)

// reLoneInjectedTag strips a lone/unclosed injected tag (wrapper or standalone),
// e.g. a bare <task-notification> whose closing tag was truncated out of the
// captured fragment. Generic names are intentionally absent here too.
var reLoneInjectedTag = regexp.MustCompile(`</?(?:task-notification|system-reminder|tool-use-error|task-id|tool-use-id|output-file)\b[^>]*>`)

// reWhitespaceRun matches one or more whitespace characters including newlines
// and tabs. Compiled once at package init.
var reWhitespaceRun = regexp.MustCompile(`\s+`)

// sanitizePromptLabel strips harness-injected metadata markup from a raw session
// prompt and collapses whitespace runs to a single space. Only KNOWN injected
// tags (knownInjectedTagAlt) are removed, so ordinary user prompt text — even
// when it contains XML/JSX/HTML snippets — is preserved.
// Steps: (1) remove known paired tags+content, (2) remove known lone tags,
// (3) collapse all whitespace to single space, (4) trim.
func sanitizePromptLabel(s string) string {
	s = reWrapperInjectedBlock.ReplaceAllString(s, "") // whole injected blocks (+ nested generic tags)
	s = rePairedInjectedTag.ReplaceAllString(s, "")    // standalone injected paired tags
	s = reLoneInjectedTag.ReplaceAllString(s, "")      // lone/unclosed injected tags
	s = reWhitespaceRun.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// SessionPromptLabel returns a human session label suitable for resume choosers.
// Prefer explicit session handoff metadata, then the latest user transcript
// message, and finally the first user transcript message. Empty means no prompt
// label is available and callers should fall back to work-item metadata.
func SessionPromptLabel(db *sql.DB, sessionID string) string {
	label, _ := sessionPromptLabel(db, sessionID)
	return label
}

// SessionPromptLabelAt returns the timestamp/order key of the prompt selected
// by SessionPromptLabel. It is intended for comparing prompt-bearing sessions
// in chooser ranking. Empty means no prompt label is available.
func SessionPromptLabelAt(db *sql.DB, sessionID string) string {
	_, at := sessionPromptLabel(db, sessionID)
	return at
}

func sessionPromptLabel(db *sql.DB, sessionID string) (string, string) {
	if db == nil || strings.TrimSpace(sessionID) == "" {
		return "", ""
	}
	rows, err := db.Query(`
WITH candidates AS (
	SELECT
		NULLIF(TRIM(last_user_query), '') AS label,
		COALESCE(last_user_query_at, '') AS label_at,
		0 AS source_rank
	FROM sessions
	WHERE session_id = ? AND TRIM(COALESCE(last_user_query, '')) <> ''
	UNION ALL
	SELECT
		NULLIF(TRIM(content), '') AS label,
		COALESCE(timestamp, '') AS label_at,
		1 AS source_rank
	FROM messages
	WHERE session_id = ? AND role = 'user' AND TRIM(content) <> ''
	UNION ALL
	SELECT
		NULLIF(TRIM(COALESCE(
			json_extract(attrs_json, '$.prompt'),
			json_extract(attrs_json, '$.text'),
			json_extract(attrs_json, '$.user_prompt')
		)), '') AS label,
		COALESCE(
			json_extract(attrs_json, '$."event.timestamp"'),
			CAST(ts_micros AS TEXT),
			''
		) AS label_at,
		2 AS source_rank
	FROM otel_signals
	WHERE session_id = ?
	  AND canonical = 'user_prompt'
	  AND TRIM(COALESCE(
		json_extract(attrs_json, '$.prompt'),
		json_extract(attrs_json, '$.text'),
		json_extract(attrs_json, '$.user_prompt'),
		''
	  )) <> ''
)
SELECT COALESCE(label, ''), COALESCE(label_at, '')
FROM candidates
WHERE label IS NOT NULL AND label <> ''
ORDER BY label_at DESC, source_rank ASC
LIMIT 10`, sessionID, sessionID, sessionID)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	for rows.Next() {
		var label, labelAt string
		if err := rows.Scan(&label, &labelAt); err != nil {
			continue
		}
		if clean := sanitizePromptLabel(label); clean != "" {
			return clean, labelAt
		}
	}
	return "", ""
}
