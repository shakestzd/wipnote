package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// optInFile is the part of a wipnote config file the opt-in lives in.
type optInFile struct {
	Delegate struct {
		Codex struct {
			Hosts *[]string `json:"dangerFullAccessHosts"`
		} `json:"codex"`
	} `json:"delegate"`
}

// readOptIn returns the hosts a config file lists; ok is false when the file
// is missing, malformed or does not set the key.
func readOptIn(path string) (hosts []string, ok bool) {
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var f optInFile
	if json.Unmarshal(data, &f) != nil || f.Delegate.Codex.Hosts == nil {
		return nil, false
	}
	for _, h := range *f.Delegate.Codex.Hosts {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts, true
}

// LoadOptIn reads delegate.codex.dangerFullAccessHosts from the user config
// (~/.wipnote/config.json) only: the opt-in widens a sandbox on one machine,
// so it never comes from a committed file. Each ignored file that sets the
// key (the project's, the local one) gets a note.
func LoadOptIn(userConfig string, ignored ...string) (hosts []string, notes []string) {
	hosts, _ = readOptIn(userConfig)
	for _, path := range ignored {
		if _, ok := readOptIn(path); ok {
			notes = append(notes, fmt.Sprintf("wipnote: ignoring delegate.codex.%s in %s; it is read only from ~/.wipnote/config.json", OptInKey, path))
		}
	}
	return hosts, notes
}
