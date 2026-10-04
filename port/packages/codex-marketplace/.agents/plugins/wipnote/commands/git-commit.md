# /wipnote:git-commit

Commit changes using Bash-copilot first, patch-coder as fallback.

## Usage

```
/wipnote:git-commit [message] [--push] [--files <list>]
```

## Parameters

- `message` (optional): Commit message. If omitted, the active agent analyzes the diff and drafts one.
- `--push`: Also push after committing.
- `--files <list>`: Specific files to stage. Default: analyze git diff and select source files, excluding `.wipnote/`.

## Examples

```
/wipnote:git-commit "feat: add user authentication"
/wipnote:git-commit --push
/wipnote:git-commit --files internal/foo.go internal/foo_test.go "fix: resolve null pointer"
```

## Instructions

Goal: commit the intended source changes with a good message, without the orchestrator running git itself. Commits trigger pre-commit hooks, conflicts, and retries, which belong in a subagent's context rather than yours.

Constraints:
- Inspect `git diff --stat HEAD; git status --short` and stage only source files; leave `.wipnote/` out unless explicitly requested. If no message was given, draft a conventional-commit message from the diff.
- Executor preference, in order: the `copilot` CLI if installed (check with `which copilot`), then `wipnote-patch-coder`, and direct `git add <files> && git commit -m "<message>"` only if both fail. Direct git is the last resort because it puts the retries in your own context, so run the availability check rather than skipping to it.
  - Copilot: `copilot -p "Stage files: <list>. Commit with message: '<message>'. Do NOT push." --allow-all-tools --no-color --add-dir . 2>&1`
  - Patch-coder: `call spawn_agent with agent_type "wipnote-patch-coder"`
- Push only when `--push` was passed (add "Then push to origin." to the prompt).

## Commit Message Format

```
<type>: <short description>

[optional body]

Co-Authored-By: <agent name> <noreply@example.com>
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`

Always append the Co-Authored-By line.

## Pre-commit Checks

This project uses `.githooks/` with pre-commit checks (go build, go vet, go test). If pre-commit fails, the commit fails — fix the reported issues and retry.

## Output Format

Report which path was used and the result:

```
Committed via: bash-copilot | patch-coder | direct git
Commit: <hash>
Files changed: <count>
Message: <message>
```
