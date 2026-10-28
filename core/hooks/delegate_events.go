package hooks

import (
	"encoding/json"

	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
)

// Delegate worker tool, question and permission events (plan-28b4a7f6
// slice 11), beside the bind events of delegate_bind.go. They let delegate
// list and wait tell working from idle_shell, and show pending asks:
//
//   - PreToolUse writes tool_start, or question for AskUserQuestion (with the
//     questions and options as a bounded summary);
//   - PostToolUse and PostToolUseFailure write tool_end;
//   - PermissionRequest writes permission_request when the prompt reaches the
//     person (wipnote's auto-allow decided nothing);
//   - UserPromptSubmit, after its prompt event, acks the newest inbox task
//     (ack/<msg-id>), which tells wait that the turn belongs to that task.
//
// Same rules as the bind: nothing outside a delegate worker, bounded reads,
// no lock, no network, and any problem writes nothing.

// delegateSummaryMax bounds an event summary, in runes.
const delegateSummaryMax = 1000

// delegateMaxMsg bounds an inbox file the ack reads; delegateAckScan bounds
// how many of the newest files it reads to find a task.
const (
	delegateMaxMsg  = 1 << 20
	delegateAckScan = 8
)

var (
	delegateNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	delegateInboxRe = regexp.MustCompile(`^([0-9]{6,})-(msg-[0-9a-f]{12,64})\.json$`)
)

func inDelegateWorker(event *CloudEvent) bool {
	return event != nil && os.Getenv(DelegateRunEnv) != ""
}

// recordDelegateToolStart is PreToolUse's event: question for an
// AskUserQuestion call, tool_start for any other tool.
func recordDelegateToolStart(event *CloudEvent, projectDir, sessionID string) {
	if !inDelegateWorker(event) {
		return
	}
	ev := DelegateEvent{Kind: "tool_start", SID: sessionID, AgentID: event.AgentID, AgentType: event.AgentType,
		Tool: event.ToolName, ToolUseID: event.ToolUseID}
	if event.ToolName == "AskUserQuestion" {
		ev.Kind, ev.Summary = "question", askSummary(event.ToolInput)
	}
	RecordDelegateEvent(projectDir, ev)
}

// delegateToolAllowed reports whether a PreToolUse result lets the call run.
func delegateToolAllowed(r *HookResult) bool {
	if r == nil || r.Decision == "deny" || r.Decision == "block" {
		return false
	}
	return r.HookSpecificOutput == nil || r.HookSpecificOutput.PermissionDecision != "deny"
}

// recordDelegateToolEnd is PostToolUse's and PostToolUseFailure's event.
func recordDelegateToolEnd(event *CloudEvent, projectDir, sessionID string) {
	if !inDelegateWorker(event) {
		return
	}
	RecordDelegateEvent(projectDir, DelegateEvent{Kind: "tool_end", SID: sessionID, AgentID: event.AgentID,
		AgentType: event.AgentType, Tool: event.ToolName, ToolUseID: event.ToolUseID})
}

// recordDelegatePermission records a permission prompt the person sees.
func recordDelegatePermission(event *CloudEvent) {
	if !inDelegateWorker(event) {
		return
	}
	sid := EnvSessionID(event.SessionID)
	if sid == "" {
		return
	}
	RecordDelegateEvent(ResolveProjectDir(event.CWD, event.SessionID), DelegateEvent{Kind: "permission_request",
		SID: sid, AgentID: event.AgentID, AgentType: event.AgentType, Tool: event.ToolName,
		ToolUseID: event.ToolUseID, Summary: SummariseInput(event.ToolName, event.ToolInput)})
}

// askSummary renders AskUserQuestion's input as "question [opt | opt]; ...".
func askSummary(input map[string]any) string {
	qs, _ := input["questions"].([]any)
	var parts []string
	for _, q := range qs {
		m, _ := q.(map[string]any)
		text, _ := m["question"].(string)
		var labels []string
		opts, _ := m["options"].([]any)
		for _, o := range opts {
			if om, ok := o.(map[string]any); ok {
				if l, _ := om["label"].(string); l != "" {
					labels = append(labels, l)
				}
			}
		}
		if len(labels) > 0 {
			text += " [" + strings.Join(labels, " | ") + "]"
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "; ")
}

func boundSummary(s string) string {
	if r := []rune(s); len(r) > delegateSummaryMax {
		return string(r[:delegateSummaryMax]) + "…"
	}
	return s
}

// delegateAckNewestTask touches ack/<id> for the newest inbox task of the
// worker named in the run's session.json, unless it is acked already.
func delegateAckNewestTask(projectDir string) {
	run := strings.TrimSpace(os.Getenv(DelegateRunEnv))
	if projectDir == "" || !delegateRunRe.MatchString(run) {
		return
	}
	root := filepath.Join(projectDir, ".wipnote", "delegate")
	name := delegateWorkerName(filepath.Join(root, "runs", run, "session.json"))
	if name == "" {
		return
	}
	mail := filepath.Join(root, "mail", name)
	ackDir := filepath.Join(mail, "ack")
	if !plainDir(filepath.Join(root, "mail")) || !plainDir(mail) || !plainDir(ackDir) {
		return
	}
	id := newestInboxTask(filepath.Join(mail, "inbox"))
	if id == "" {
		return
	}
	path := filepath.Join(ackDir, id)
	if _, err := os.Lstat(path); err == nil {
		return
	}
	_ = filelock.WriteFileAtomic(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// delegateWorkerName reads the worker name from a plain, small session.json.
func delegateWorkerName(path string) string {
	var info struct {
		Name string `json:"name"`
	}
	if !readSmallJSON(path, 64<<10, &info) || !delegateNameRe.MatchString(info.Name) {
		return ""
	}
	return info.Name
}

// newestInboxTask is the id of the newest task among the newest few inbox
// files, or "".
func newestInboxTask(inbox string) string {
	if !plainDir(inbox) {
		return ""
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return ""
	}
	type file struct {
		seq      int64
		id, name string
	}
	var files []file
	for _, e := range entries {
		if sub := delegateInboxRe.FindStringSubmatch(e.Name()); sub != nil && e.Type().IsRegular() {
			seq, _ := strconv.ParseInt(sub[1], 10, 64)
			files = append(files, file{seq, sub[2], e.Name()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].seq > files[j].seq })
	for i, f := range files {
		if i >= delegateAckScan {
			break
		}
		var msg struct{ ID, Type string }
		if readSmallJSON(filepath.Join(inbox, f.name), delegateMaxMsg, &msg) && msg.ID == f.id && msg.Type == "task" {
			return f.id
		}
	}
	return ""
}

func plainDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}

func readSmallJSON(path string, limit int64, v any) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > limit {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, v) == nil
}
