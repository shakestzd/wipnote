package session

import (
	"regexp"
	"strings"
)

// Crumb separates a parent label from the worker name in a Claude session
// name (U+203A with a space each side).
const Crumb = " › "

// maxCrumbName bounds a plain parent name used as a label.
const maxCrumbName = 24

var crumbItemID = regexp.MustCompile(`^(?:feat|bug|spk|chr)-[0-9a-f]{8}`)

// ParentLabel is the label a worker's Claude session name starts with, from
// the caller's current session name (empty when unreadable) and its session
// id: a name that already holds a breadcrumb stays whole so nested chains
// grow; a name starting with a work item id becomes that id; any other name
// is cut to 24 runes; with no name, the session id's first 8 characters.
// With no caller at all the label is empty.
func ParentLabel(callerName, callerSession string) string {
	name := strings.TrimSpace(callerName)
	switch {
	case strings.Contains(name, Crumb):
		return name
	case name != "":
		if id := crumbItemID.FindString(name); id != "" {
			return id
		}
		if r := []rune(name); len(r) > maxCrumbName {
			return string(r[:maxCrumbName])
		}
		return name
	case callerSession == "":
		return ""
	case len(callerSession) > 8:
		return callerSession[:8]
	}
	return callerSession
}

// ClaudeSessionName is the --name of a new Claude worker: the parent label,
// Crumb, then the worker name; the bare name with no label.
func ClaudeSessionName(label, name string) string {
	if label == "" {
		return name
	}
	return label + Crumb + name
}
