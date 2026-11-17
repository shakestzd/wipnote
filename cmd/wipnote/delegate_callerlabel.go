package main

import (
	"bufio"
	"encoding/json"
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

func lastAgentName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	last := ""
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"agent-name"`) {
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
