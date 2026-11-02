package session

import (
	"strings"

	"github.com/shakestzd/wipnote/core/slug"
)

// terminalName is the hc terminal name for a worker: the worker name with the
// work title's slug appended, so a pane list says what each worker is doing.
// A name that already carries the slug (an auto-generated
// <item-id>-<slug>, even one cut short) comes back unchanged, as does any name
// when the title is empty or no room is left. The result is at most
// slug.NameWidth characters and never ends in '-'.
func terminalName(name, workTitle string) string {
	full := slug.Make(workTitle, slug.NameWidth)
	if full == "" || strings.Contains(name, full) {
		return name
	}
	for k := len(full); k > 0; k-- {
		if p := slug.Make(workTitle, k); p != "" && strings.HasSuffix(name, "-"+p) {
			return name
		}
	}
	room := slug.NameWidth - len(name) - 1
	if room <= 0 {
		return name
	}
	s := slug.Make(workTitle, room)
	if s == "" {
		return name
	}
	return strings.TrimRight(name+"-"+s, "-")
}
