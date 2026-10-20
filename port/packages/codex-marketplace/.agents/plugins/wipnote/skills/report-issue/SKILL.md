---
name: wipnote:report-issue
description: Report a wipnote bug or issue to the development team from any supported harness. Use when the user asks to report an issue, file a bug, or says the report-issue slash command is unavailable.
disable-model-invocation: true
---

# wipnote Report Issue

Use this skill to help the user file a concise, safe issue for `shakestzd/wipnote`.

## Instructions

1. Ask for a short title and description if the user has not already provided them.
2. Gather safe environment context when available:
   - `wipnote version`
   - `go version` (optional; only for build or install issues)
   - OS via `uname -s -r` or the platform equivalent
   - active harness version, such as `claude --version`, `codex --version`, or `agy --version`
3. Ask whether to include steps to reproduce, expected behavior, actual behavior, and any error text.
4. Scrub secrets, API keys, tokens, private file contents, and personal data before drafting or submitting.
5. Write the draft to a file in your scratchpad directory (or a temp file) using the template below as the default shape.
6. If `gh` is authenticated and the user wants you to submit it, create the issue:

```bash
gh issue create --repo shakestzd/wipnote --title "<Type>: <title>" --body-file <draft-file>
```

   The title prefix follows the issue type: `Bug:`, `Feature:`, `Docs:`, and so on.

7. If GitHub submission is unavailable, provide a ready-to-paste issue body and link to https://github.com/shakestzd/wipnote/issues/new.

## Issue Body Template

This is the default shape for a bug. Adapt the headings for other issue types and drop sections that do not apply.

```markdown
## Bug Report

**Description:** <user description>

### Environment
- wipnote version: <version>
- Go version (optional): <version>
- OS: <os>
- Harness version: <version>

### Steps to Reproduce
<steps>

### Expected Behavior
<expected>

### Actual Behavior
<actual>

### Error Output
~~~text
<error messages>
~~~

---
Reported via wipnote issue reporter
```
