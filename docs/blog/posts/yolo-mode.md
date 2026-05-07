---
date: 2026-04-14
authors:
  - shakes
categories:
  - Features
slug: yolo-mode-autonomous-guardrails
---

# YOLO Mode: Autonomous AI Development With Engineering Guardrails

The question every developer using AI coding tools eventually asks: "Can I just let it run?" The appeal of autonomous AI development is obvious: you describe what you want, walk away, and come back to a working feature. The fear is equally obvious: what if it makes a mess?

Claude Code's answer is auto mode, a server-side classifier that screens every tool call for security threats like data exfiltration, production deployments, and force-pushes to main. It's a good answer to the security question. But it's only available on Team, Enterprise, or API plans, requires admin enablement, and only works with the Anthropic API provider. And it doesn't address a different question entirely: is the AI doing *quality* development work?

wipnote's YOLO mode answers the quality question.

<!-- more -->

## The setup

YOLO mode operates on `bypassPermissions`, the most permissive Claude Code mode, where all permission prompts are skipped. On its own, this skips all user-facing permission prompts. The Claude Code documentation says it "offers no protection against prompt injection or unintended actions" and recommends it only for isolated containers. The model's safety training still applies, but there are no structural quality gates.

But YOLO mode doesn't run on `bypassPermissions` alone. It layers 9 deterministic guardrails on top, all implemented as Go hooks that fire on every tool call. These hooks are the safety net between Claude and your codebase.

## The 9 guardrails

### 1. Work item guard

Blocks `Write`, `Edit`, and `MultiEdit` operations if no active feature ID is linked to the session. Every change must trace to a tracked work item. No anonymous code modifications.

### 2. Worktree guard

Blocks all writes on `main` or `master` branch. Every YOLO session gets its own git worktree on a dedicated branch. The worst case is a bad branch you throw away, not damage to your mainline.

### 3. Research guard

Blocks any write if no `Read`, `Grep`, or `Glob` tool has run in the session. Forces the agent to look at the existing code before modifying it. This single guard prevents a surprising number of "the agent rewrote everything from scratch instead of editing what was there" incidents.

### 4. Test guard

Blocks `git commit` if no test command (`go test`, `pytest`, `uv run ruff`) has run in the session. You can write code all day, but you can't ship it without testing.

### 5. Diff review guard

Blocks `git commit` if no `git diff` has been run in the session. The agent must look at what it's about to commit. This catches cases where the agent thinks it made a small change but actually staged something much larger.

### 6. Budget guard

Blocks `git commit` if the staged diff exceeds hard limits: 20 files or 600 lines added. Merge commits are exempt. There's also an advisory threshold at 10 files or 300 lines that triggers a warning without blocking.

This exists because I once watched an agent stage a 47-file commit that refactored half the codebase when I asked it to fix a typo. The budget guard makes oversized commits structurally impossible.

### 7. UI validation guard

Blocks `git commit` if any UI files (`.html`, `.css`, `.js`, `.ts`, `.tsx`, `.vue`, `.svelte`) were modified but no screenshot tool was called. If you changed the UI, you need to verify it looks right.

### 8. Steps guard

Warns (soft block) when `wipnote feature start` is called on a feature that has zero implementation steps. Encourages the agent to think about the approach before starting to code.

### 9. Code health guard

Warns (non-blocking) when a file being written already exceeds 500 lines. The warning is non-blocking to allow refactoring, but it surfaces the problem so the agent (and you) know the file is getting large.

## The embedded workflow

Beyond hooks, YOLO mode injects an 8-step development workflow into the system prompt:

1. Create or claim a work item
2. Research the codebase
3. Write a spec with acceptance criteria
4. Write tests first (TDD)
5. Implement the feature
6. Run quality gates (`go build && go vet && go test`)
7. Validate UI changes visually
8. Review the diff and commit

The hooks enforce the critical steps (research, testing, diff review, budget). The prompt guides the agent through the full workflow. Together they produce commits that look like they came from a disciplined developer, not a runaway AI.

## How it compares to auto mode

Auto mode and YOLO mode solve different problems:

| Concern | Auto Mode | YOLO Mode |
|---------|-----------|-----------|
| **Primary focus** | Security threats | Development quality |
| **Guardrail type** | ML classifier (probabilistic, 83% recall) | Go hooks (deterministic, 100% reliable) |
| **Branch discipline** | Blocks force-push to main | Blocks ALL writes on main |
| **Commit hygiene** | Not addressed | Tests + diff review + budget required |
| **Work attribution** | Not addressed | Blocks writes without tracked feature |
| **Research before writing** | Not addressed | Enforced |
| **Availability** | Team/Enterprise/API only | Any plan, any provider, any model |
| **Prompt injection defense** | Classifier strips tool results | Not addressed |

They're complementary, not competing. Auto mode catches security threats: exfiltration, IAM changes, production deployments. YOLO mode catches quality issues: untested code, oversized commits, unresearched modifications, unattributed work. The ideal setup would layer both, though that's not currently wired up since auto mode requires a subscription tier I don't have access to.

## Worktree isolation

Every YOLO session creates a dedicated git worktree:

- Track worktrees: `.claude/worktrees/<trackID>/` on branch `trk-<trackID>`
- Feature worktrees: `.claude/worktrees/<featureID>/` on branch `yolo-<featureID>`
- Sub-agent worktrees: nested under the track worktree on branch `agent-<trackID>-<taskName>`

This provides structural safety independent of the hooks. An agent that produces garbage doesn't pollute your working tree; it pollutes its own isolated branch that you can inspect and discard. The `.wipnote/` directory is excluded from worktrees via git's local exclude file, so all work item operations resolve to the main project's state.

## The origin story

I didn't design these guardrails theoretically. Every one of them exists because of a real incident:

- The work item guard: agents were writing code with no traceable purpose
- The worktree guard: an agent committed directly to main during an autonomous run
- The research guard: agents kept reimplementing functions that already existed
- The budget guard: that 47-file commit I mentioned
- The UI validation guard: an agent shipped a CSS change that broke the entire dashboard layout

YOLO mode is scar tissue turned into engineering discipline. The philosophy is simple: enforce the same standards a good engineer follows: research first, test before committing, don't touch main, keep changes focused, and make every modification traceable to a reason.

The difference is that a human developer follows these practices by habit and judgment. An autonomous AI needs structural enforcement.
