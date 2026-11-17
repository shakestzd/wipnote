package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// callerLabel is the breadcrumb label for a worker spawned by the Claude
// session callerID: from that session's current name when readable.
func callerLabel(callerID string) string {
	return session.ParentLabel(claudeSessionName(claudeProjectsDir(), callerID), callerID)
}

// claudeSessionName is the last agent-name entry of the session's transcript
// (<projects>/<encoded dir>/<id>.jsonl), or "" when there is none. The
// session may have run in any worktree, so every project folder is tried.
func claudeSessionName(projectsDir, sessionID string) string {
	if projectsDir == "" || sessionID == "" || strings.ContainsAny(sessionID, `/\`) {
		return ""
	}
	paths, _ := filepath.Glob(filepath.Join(projectsDir, "*", sessionID+".jsonl"))
	for _, p := range paths {
		if name := lastAgentName(p); name != "" {
			return name
		}
	}
	return ""
}

// tailBytes is how much of a transcript's end is read first. Claude Code
// re-writes the agent-name entry through the session, so the current name is
// almost always near the end; a parent transcript can be ~100 MB, and a
// spawn should not scan all of it.
const tailBytes = 1 << 20

// lastAgentName is the session's latest agent-name: from the file's last
// tailBytes when one is there, else from the whole file.
func lastAgentName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > tailBytes {
		if _, err := f.Seek(st.Size()-tailBytes, io.SeekStart); err == nil {
			if name := scanAgentName(f); name != "" {
				return name
			}
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return ""
		}
	}
	return scanAgentName(f)
}

// scanAgentName returns the last agent-name entry in r. A partial first
// line (after a seek) fails to parse and is skipped.
func scanAgentName(r io.Reader) string {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	last := ""
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"agent-name"`)) {
			continue
		}
		var e struct {
			Type      string `json:"type"`
			AgentName string `json:"agentName"`
		}
		if json.Unmarshal(line, &e) == nil && e.Type == "agent-name" && e.AgentName != "" {
			last = e.AgentName
		}
	}
	return last
}
