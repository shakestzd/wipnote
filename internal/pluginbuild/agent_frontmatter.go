package pluginbuild

import (
	"bytes"
	"fmt"
	"log"
	"sort"

	"gopkg.in/yaml.v3"
)

var sharedAgentFrontmatterOrder = []string{
	"name",
	"description",
	"model",
	"color",
	"tools",
	"disallowedTools",
	"maxTurns",
	"skills",
	"initialPrompt",
	"memory",
	"timeout_mins",
}

var sharedAgentFrontmatterFields = setOf(
	"name",
	"description",
	"model",
	"color",
	"tools",
	"disallowedTools",
	"maxTurns",
	"skills",
	"initialPrompt",
	"memory",
	"timeout_mins",
)

var harnessAgentFrontmatterAllowlist = map[string]map[string]struct{}{
	"claude": setOf(
		"name",
		"description",
		"model",
		"color",
		"tools",
		"maxTurns",
		"memory",
	),
	"codex": setOf(
		"name",
		"description",
		"model",
		"tools",
		"initialPrompt",
	),
	"gemini": setOf(
		"name",
		"description",
		"model",
		"tools",
		"maxTurns",
		"timeout_mins",
	),
}

func parseAgentFrontmatter(raw []byte) (fm map[string]any, body []byte, hasFM bool, err error) {
	fmRaw, body, hasFM := splitFrontmatter(raw)
	if !hasFM {
		return nil, raw, false, nil
	}
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return nil, nil, false, fmt.Errorf("parse frontmatter YAML: %w", err)
	}
	if fm == nil {
		fm = map[string]any{}
	}
	return fm, body, true, nil
}

func filterAgentFrontmatter(filename, harness string, fm map[string]any) map[string]any {
	allow, ok := harnessAgentFrontmatterAllowlist[harness]
	if !ok {
		return fm
	}
	filtered := make(map[string]any, len(fm))
	for _, key := range sortedKeys(fm) {
		value := fm[key]
		if _, known := sharedAgentFrontmatterFields[key]; !known {
			log.Printf("pluginbuild: agent %s: frontmatter field %q is not recognized in shared source; omitting from %s output", filename, key, harness)
			continue
		}
		if _, allowed := allow[key]; !allowed {
			log.Printf("pluginbuild: agent %s: frontmatter field %q is unsupported for %s output; omitting", filename, key, harness)
			continue
		}
		filtered[key] = value
	}
	return filtered
}

func marshalAgentFrontmatter(fm map[string]any) ([]byte, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range sharedAgentFrontmatterOrder {
		value, ok := fm[key]
		if !ok {
			continue
		}
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
		valueNode := &yaml.Node{}
		if err := valueNode.Encode(value); err != nil {
			return nil, fmt.Errorf("encode frontmatter field %q: %w", key, err)
		}
		node.Content = append(node.Content, keyNode, valueNode)
	}
	return yaml.Marshal(node)
}

func renderAgentMarkdown(fm map[string]any, body []byte) ([]byte, error) {
	if len(fm) == 0 {
		return body, nil
	}
	fmBytes, err := marshalAgentFrontmatter(fm)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmBytes)
	buf.WriteString("---\n")
	buf.Write(body)
	return buf.Bytes(), nil
}

func setOf(keys ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
