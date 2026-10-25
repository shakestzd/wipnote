package hooks

import (
	"fmt"
)

// summarizeToolOutput extracts the full output string from a tool result for
// commit parsing (we need more than the 200-char summariseOutput truncation).
func summarizeToolOutput(result map[string]any) string {
	if result == nil {
		return ""
	}
	for _, key := range []string{"output", "content", "result"} {
		if v, ok := result[key].(string); ok {
			return v
		}
	}
	return ""
}

// isSuccess returns false when the tool result contains an explicit error flag.
func isSuccess(result map[string]any) bool {
	if result == nil {
		return true
	}
	if v, ok := result["is_error"].(bool); ok && v {
		return false
	}
	return true
}

// summariseOutput extracts a short string from the tool result map.
func summariseOutput(result map[string]any) string {
	if result == nil {
		return ""
	}
	for _, key := range []string{"output", "content", "result", "error"} {
		if v, ok := result[key].(string); ok && v != "" {
			if len(v) > 200 {
				v = v[:200] + "…"
			}
			return v
		}
	}
	return ""
}

// summariseToolOutput builds a tool-specific structured output summary that
// captures key metadata (file path, success, content length) rather than raw
// output text. Falls back to summariseOutput for unrecognised tools.
func summariseToolOutput(toolName string, input map[string]any, result map[string]any, success bool) string {
	switch toolName {
	case "Read":
		return summariseReadOutput(input, result, success)
	case "Write":
		return summariseWriteOutput(input, success)
	case "Edit", "MultiEdit":
		return summariseEditOutput(input, success)
	case "Glob":
		return summariseGlobOutput(result, success)
	case "Grep":
		return summariseGrepOutput(result, success)
	default:
		return summariseOutput(result)
	}
}

func summariseReadOutput(input, result map[string]any, success bool) string {
	filePath := extractFilePath(input)
	if filePath == "" {
		filePath = "unknown"
	}
	if !success {
		return fmt.Sprintf("%s (error)", filePath)
	}
	// Count lines in content to report size.
	content := ""
	for _, key := range []string{"output", "content", "result"} {
		if v, ok := result[key].(string); ok {
			content = v
			break
		}
	}
	lines := countLines(content)
	return fmt.Sprintf("%s (ok, %d lines)", filePath, lines)
}

func summariseWriteOutput(input map[string]any, success bool) string {
	filePath := extractFilePath(input)
	if filePath == "" {
		filePath = "unknown"
	}
	if !success {
		return fmt.Sprintf("%s (error)", filePath)
	}
	return fmt.Sprintf("%s (written)", filePath)
}

func summariseEditOutput(input map[string]any, success bool) string {
	filePath := extractFilePath(input)
	if filePath == "" {
		filePath = "unknown"
	}
	if !success {
		return fmt.Sprintf("%s (error)", filePath)
	}
	return fmt.Sprintf("%s (edited)", filePath)
}

func summariseGlobOutput(result map[string]any, success bool) string {
	if !success {
		return "glob (error)"
	}
	content := ""
	for _, key := range []string{"output", "content", "result"} {
		if v, ok := result[key].(string); ok {
			content = v
			break
		}
	}
	n := countLines(content)
	return fmt.Sprintf("%d files matched", n)
}

func summariseGrepOutput(result map[string]any, success bool) string {
	if !success {
		return "grep (error)"
	}
	content := ""
	for _, key := range []string{"output", "content", "result"} {
		if v, ok := result[key].(string); ok {
			content = v
			break
		}
	}
	n := countLines(content)
	return fmt.Sprintf("%d matches", n)
}

// countLines returns the number of non-empty lines in s.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for i := range s {
		if s[i] == '\n' {
			n++
		}
	}
	// Don't count trailing newline as an extra line.
	if len(s) > 0 && s[len(s)-1] == '\n' {
		n--
	}
	return n
}
