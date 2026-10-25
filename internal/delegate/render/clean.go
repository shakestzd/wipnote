package render

import (
	"regexp"
	"strings"
)

// The Codex noise rules (the mod's clean.awk): the raw log keeps everything,
// clean.log and clean.md drop these.
var (
	versionRe  = regexp.MustCompile(`^OpenAI Codex v[0-9]`)
	ruleRe     = regexp.MustCompile(`^-+$`)
	mcpListRe  = regexp.MustCompile(`^mcp: codex/list_mcp_resource(s|_templates) (started|\(completed\))$`)
	clampRe    = regexp.MustCompile(`^warning: clamping .* hook timeout`)
	logLevelRe = regexp.MustCompile(`^[0-9T:.Z-]+ +(INFO|DEBUG|WARN) `)
)

// mcpLogNote replaces every rmcp transport error, once per stream.
const mcpLogNote = "! an MCP server was unreachable (rmcp transport closed); raw.log has the detail"

// cleaner applies the noise rules to one stream, a line at a time. review
// is set for a Codex stream (never the rest of a chained command): codex
// review's start-up banner and its own MCP resource listing are dropped too.
type cleaner struct {
	review  bool
	banners bool // a banner was seen (only the first is dropped)
	banner  bool // inside it
	rules   int
	mcp     bool
}

// line returns the line to keep, or false.
func (c *cleaner) line(l string) (string, bool) {
	switch {
	case c.review && !c.banners && versionRe.MatchString(l):
		c.banner, c.banners, c.rules = true, true, 0
		return "", false
	case c.banner && ruleRe.MatchString(l):
		c.rules++
		if c.rules == 2 {
			c.banner = false
		}
		return "", false
	case c.banner:
		return "", false
	case c.review && mcpListRe.MatchString(l):
		return "", false
	case strings.HasPrefix(l, "hook: "), clampRe.MatchString(l), logLevelRe.MatchString(l):
		return "", false
	case strings.Contains(l, "rmcp::transport::worker"):
		if c.mcp {
			return "", false
		}
		c.mcp = true
		return mcpLogNote, true
	}
	return l, true
}

// joinFence runs neighbouring text fences of clean.md together (the mod's
// joinfence.awk): Codex fences each plain line on its own, so codex review
// would be one fence per line. When a text block closes and, after one
// blank line, a text block of the same fence opens, the closing fence, the
// blank and the opening fence are dropped. Other blocks keep their fences;
// each part's backtick runs are already shorter than its fence, so the
// joined block cannot be broken out of.
type joinFence struct {
	held  string // a text block's closing fence, held back
	blank bool   // and the one blank line after it
	open  string // the fence of the block being read
	text  bool   // that block is a text block
}

// line takes one line of clean.md and returns what to write.
func (j *joinFence) line(l string) string {
	if j.held != "" && !j.blank && l == "" {
		j.blank = true
		return ""
	}
	if j.held != "" && j.blank && l == j.held+"text" {
		j.open, j.text, j.held, j.blank = j.held, true, "", false
		return ""
	}
	out := j.flush()
	switch {
	case j.open == "" && strings.HasPrefix(l, "```"):
		j.open = l[:len(l)-len(strings.TrimLeft(l, "`"))]
		j.text = l == j.open+"text"
	case j.open != "" && l == j.open:
		j.open = ""
		if j.text {
			j.held, j.blank = l, false
			return out
		}
	}
	return out + l + "\n"
}

// flush writes a held fence (and its blank line).
func (j *joinFence) flush() string {
	if j.held == "" {
		return ""
	}
	out := j.held + "\n"
	if j.blank {
		out += "\n"
	}
	j.held, j.blank = "", false
	return out
}
