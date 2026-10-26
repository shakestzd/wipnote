package gitwatch

import "regexp"

// WorkItemPattern matches a wipnote work-item id: feat-, bug- or spk-
// followed by eight lower-case hex digits.
var WorkItemPattern = regexp.MustCompile(`\b(feat|bug|spk)-[0-9a-f]{8}\b`)

// WorkItemID returns the first work-item id in branch, else in title, else "".
func WorkItemID(branch, title string) string {
	if id := WorkItemPattern.FindString(branch); id != "" {
		return id
	}
	return WorkItemPattern.FindString(title)
}
