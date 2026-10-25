package pluginbuild

import (
	"strings"
)

func rewriteCodexDelegationSyntax(content string, knownRoles map[string]struct{}) string {
	return rewriteDelegationSyntax(content, delegationTargetCodex, knownRoles)
}

func rewriteGeminiDelegationSyntax(content string, knownRoles map[string]struct{}) string {
	return rewriteDelegationSyntax(content, delegationTargetGemini, knownRoles)
}

type delegationTarget string

const (
	delegationTargetCodex  delegationTarget = "codex"
	delegationTargetGemini delegationTarget = "gemini"
)

func rewriteDelegationSyntax(content string, target delegationTarget, knownRoles map[string]struct{}) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inDelegationBlock := false
	var block []string
	var blockIndent string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if isDelegationBlockStart(trimmed) {
			inDelegationBlock = true
			block = []string{line}
			blockIndent = indent
			out = append(out, indent+delegationBlockHeader(target))
			if target != delegationTargetCodex {
				if field := delegationFieldFromOpener(trimmed); field != "" {
					out = append(out, indent+"    "+rewriteDelegationField(field, target))
				}
			}
			continue
		}
		if inDelegationBlock && isDelegationBlockEnd(trimmed) {
			inDelegationBlock = false
			if target == delegationTargetCodex {
				block = append(block, line)
				out = append(out, blockIndent+codexDelegationBlockProse(strings.Join(block, "\n"), knownRoles))
				block = nil
			}
			continue
		}
		if inDelegationBlock {
			block = append(block, line)
			if target != delegationTargetCodex {
				out = append(out, rewriteDelegationField(line, target))
			}
			continue
		}
		out = append(out, rewriteInlineDelegationCall(line, target, knownRoles))
	}
	return strings.Join(out, "\n")
}

func isDelegationBlockStart(trimmed string) bool {
	if !(strings.HasPrefix(trimmed, "Task(") || strings.HasPrefix(trimmed, "Agent(")) {
		return false
	}
	return !strings.Contains(trimmed, ")")
}

func isDelegationBlockEnd(trimmed string) bool {
	return trimmed == ")" || trimmed == "),"
}

func delegationFieldFromOpener(trimmed string) string {
	idx := strings.Index(trimmed, "(")
	if idx < 0 {
		return ""
	}
	field := strings.TrimSpace(trimmed[idx+1:])
	if field == "" {
		return ""
	}
	field = strings.TrimSuffix(field, ")")
	field = strings.TrimSpace(field)
	return field
}

func delegationBlockHeader(target delegationTarget) string {
	switch target {
	case delegationTargetCodex:
		return "Call spawn_agent with:"
	case delegationTargetGemini:
		return "Use Gemini agent invocation with:"
	default:
		return "Delegate with:"
	}
}

func rewriteDelegationField(line string, target delegationTarget) string {
	line = strings.ReplaceAll(line, "prompt=", "message=")
	for _, key := range []string{"subagent_type=", "agent="} {
		idx := strings.Index(line, key+"\"")
		if idx < 0 {
			continue
		}
		valueStart := idx + len(key) + 1
		valueEnd := strings.Index(line[valueStart:], "\"")
		if valueEnd < 0 {
			return line
		}
		valueEnd += valueStart
		role := line[valueStart:valueEnd]
		var replacement string
		switch target {
		case delegationTargetCodex:
			if isWipnoteRole(role) {
				replacement = "agent_type=\"" + codexAgentType(role) + "\""
			} else {
				replacement = "workflow=\"" + role + "\""
			}
		case delegationTargetGemini:
			if isWipnoteRole(role) {
				replacement = "agent=\"" + geminiAgentInvocation(role) + "\""
			} else {
				replacement = "workflow=\"" + role + "\""
			}
		default:
			replacement = "agent=\"" + role + "\""
		}
		return line[:idx] + replacement + line[valueEnd+1:]
	}
	switch target {
	case delegationTargetCodex:
		line = strings.ReplaceAll(line, "subagent_type=", "agent_type=")
	case delegationTargetGemini:
		line = strings.ReplaceAll(line, "subagent_type=", "agent=")
	}
	return line
}

func rewriteInlineDelegationCall(line string, target delegationTarget, knownRoles map[string]struct{}) string {
	for {
		taskIdx := strings.Index(line, "Task(")
		agentIdx := strings.Index(line, "Agent(")
		idx := firstNonNegative(taskIdx, agentIdx)
		if idx < 0 {
			return line
		}
		endRel := strings.Index(line[idx:], ")")
		if endRel < 0 {
			return line
		}
		end := idx + endRel + 1
		replacement := delegationProse(line[idx:end], target, knownRoles)
		line = line[:idx] + replacement + line[end:]
	}
}

func delegationProse(call string, target delegationTarget, knownRoles map[string]struct{}) string {
	role := delegationRole(call)
	switch target {
	case delegationTargetCodex:
		if role == "" {
			return "call spawn_agent with the appropriate agent_type"
		}
		if !isKnownWipnoteRole(role, knownRoles) {
			return "use the " + delegationWorkflowName(role) + " workflow described here"
		}
		return "call spawn_agent with agent_type \"" + codexAgentType(role) + "\""
	case delegationTargetGemini:
		if role == "" {
			return "use the appropriate Gemini agent invocation"
		}
		if !isKnownWipnoteRole(role, knownRoles) {
			return "use the " + delegationWorkflowName(role) + " workflow described here"
		}
		return "use " + geminiAgentInvocation(role)
	default:
		if role == "" {
			return "delegate to the appropriate agent"
		}
		return "delegate to " + role
	}
}

func codexDelegationBlockProse(call string, knownRoles map[string]struct{}) string {
	role := delegationRole(call)
	details := delegationDetails(call)
	if !isKnownWipnoteRole(role, knownRoles) {
		if role == "" {
			if details != "" {
				return "Call spawn_agent with: " + ensureTerminalPeriod(details)
			}
			return "Use the appropriate workflow described here."
		}
		if details != "" {
			return "Use the " + delegationWorkflowName(role) + " workflow described here with: " + ensureTerminalPeriod(details)
		}
		return "Use the " + delegationWorkflowName(role) + " workflow described here."
	}
	if details == "" {
		return "Call spawn_agent with agent_type \"" + codexAgentType(role) + "\"."
	}
	return "Call spawn_agent with agent_type \"" + codexAgentType(role) + "\" and message containing: " + ensureTerminalPeriod(details)
}

func ensureTerminalPeriod(s string) string {
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}

func delegationDetails(call string) string {
	lines := strings.Split(call, "\n")
	parts := []string{}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if i == 0 {
			trimmed = delegationFieldFromOpener(trimmed)
		}
		if trimmed == "" || isDelegationBlockEnd(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "prompt=") || strings.HasPrefix(trimmed, "message=") {
			detail, next := delegationMultilineFieldDetail(lines, i, trimmed)
			if detail != "" {
				parts = append(parts, detail)
			}
			i = next
			continue
		}
		if strings.HasPrefix(trimmed, "subagent_type=") || strings.HasPrefix(trimmed, "agent=") {
			if detail := delegationAgentFieldDetail(trimmed); detail != "" {
				parts = append(parts, detail)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "description=") ||
			strings.HasPrefix(trimmed, "isolation=") ||
			strings.HasPrefix(trimmed, "run_in_background=") {
			parts = append(parts, strings.TrimRight(trimmed, ","))
		}
	}
	return strings.Join(parts, "; ")
}

func delegationAgentFieldDetail(trimmed string) string {
	trimmed = strings.TrimRight(trimmed, ",")
	for _, prefix := range []string{"subagent_type=", "agent="} {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			return ""
		}
		return "agent_type=" + value
	}
	return ""
}

func delegationMultilineFieldDetail(lines []string, start int, first string) (string, int) {
	keyEnd := strings.Index(first, "=")
	if keyEnd < 0 {
		return strings.TrimRight(first, ","), start
	}
	key := first[:keyEnd]
	value := strings.TrimSpace(first[keyEnd+1:])
	if !strings.HasPrefix(value, `"""`) {
		return strings.TrimRight(first, ","), start
	}
	value = strings.TrimPrefix(value, `"""`)
	parts := []string{}
	if done, cleaned := trimTripleQuoteEnd(value); done {
		if cleaned != "" {
			parts = append(parts, cleaned)
		}
		return key + "=" + strings.Join(parts, "\n"), start
	}
	if value != "" {
		parts = append(parts, value)
	}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if done, cleaned := trimTripleQuoteEnd(trimmed); done {
			if cleaned != "" {
				parts = append(parts, cleaned)
			}
			return key + "=" + strings.Join(parts, "\n"), i
		}
		parts = append(parts, strings.TrimRight(trimmed, ","))
	}
	return key + "=" + strings.Join(parts, "\n"), len(lines) - 1
}

func trimTripleQuoteEnd(value string) (bool, string) {
	trimmed := strings.TrimSpace(value)
	for _, suffix := range []string{`""",`, `"""`} {
		if strings.HasSuffix(trimmed, suffix) {
			cleaned := strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
			return true, strings.TrimRight(cleaned, ",")
		}
	}
	return false, value
}

func isWipnoteRole(role string) bool {
	return strings.HasPrefix(role, "wipnote:") || strings.HasPrefix(role, "wipnote-")
}

func isKnownWipnoteRole(role string, knownRoles map[string]struct{}) bool {
	if !isWipnoteRole(role) {
		return false
	}
	role = strings.TrimPrefix(role, "wipnote:")
	role = strings.TrimPrefix(role, "wipnote-")
	_, ok := knownRoles[role]
	return ok
}

func delegationWorkflowName(role string) string {
	role = strings.TrimPrefix(role, "wipnote:")
	role = strings.TrimPrefix(role, "wipnote-")
	return role
}

func codexAgentType(role string) string {
	role = strings.TrimPrefix(role, "wipnote:")
	if strings.HasPrefix(role, "wipnote-") {
		return role
	}
	return "wipnote-" + role
}

func geminiAgentInvocation(role string) string {
	role = strings.TrimPrefix(role, "wipnote:")
	role = strings.TrimPrefix(role, "wipnote-")
	return "@" + role
}

func firstNonNegative(a, b int) int {
	switch {
	case a < 0:
		return b
	case b < 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func delegationRole(call string) string {
	for _, key := range []string{`subagent_type="`, `agent="`} {
		idx := strings.Index(call, key)
		if idx < 0 {
			continue
		}
		rest := call[idx+len(key):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			return ""
		}
		return rest[:end]
	}
	return ""
}
