package slug

import "strings"

// NameWidth is the most characters a worker or session name keeps, so a
// pane list or session list shows it whole (plan-28b4a7f6 slice 15).
const NameWidth = 32

// WorkerName is the harness name for a session that works on a work item:
// "<item-id>-<slug of title>", lowercased and cut to NameWidth on a word
// boundary. A title with no slug gives the id alone; no id gives "", so a
// caller keeps whatever name it would have used without a work item.
func WorkerName(id, title string) string {
	id = Make(strings.TrimSpace(id), NameWidth)
	if id == "" {
		return ""
	}
	room := NameWidth - len(id) - 1
	if room <= 0 {
		return id
	}
	if s := Make(title, room); s != "" {
		return id + "-" + s
	}
	return id
}

// WorkTitle is "<id><sep><title>" with the title's whitespace collapsed, cut
// to width runes (the last one an ellipsis when cut). An empty title gives
// the id alone and an empty id gives "". It is the one rule behind herdr
// pane labels and Claude session titles.
func WorkTitle(id, sep, title string, width int) string {
	label := strings.TrimSpace(id)
	if label == "" {
		return ""
	}
	if t := strings.Join(strings.Fields(title), " "); t != "" {
		label += sep + t
	}
	if width > 0 {
		label = Truncate(label, width)
	}
	return label
}
