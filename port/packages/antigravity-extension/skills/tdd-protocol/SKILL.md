---
name: wipnote:tdd-protocol
description: TDD steps for a delegated implementation task: failing test first, quality gate, commit format, completion. Use when implementing a feature or bug fix as a coder agent.
disable-model-invocation: true
---

# TDD Protocol for Sub-Agent Implementation

Use this skill for any implementation task dispatched by `/wipnote:execute`. `<kind>` below is `feature`, `bug` or `spike`, matching the work item ID in your brief.

## Step 1: Attribution before any code

```bash
wipnote <kind> start <id>
```

Run this as the first command, before reading files or writing tests.

## Step 2: Write failing tests first

1. Write tests that express the acceptance criteria.
2. Run the test phase of the gate from `/wipnote:code-quality-skill`.
3. Confirm the tests compile and fail for the right reason: the implementation does not exist yet.

Do not write implementation code until tests exist and fail.

## Step 3: Implement until tests pass

Write the minimum implementation that makes the tests pass. Do not add code that no test covers.

## Step 4: Quality gate before commit

Run the gate from `/wipnote:code-quality-skill`. Fix every failure, including pre-existing ones in files you touched. Re-run the gate. Repeat until all phases pass.

## Step 5: Commit

Use a conventional commit with the work item ID:

```
git commit -m "feat(<scope>): <description> (<id>)"
```

Add the attribution trailer your harness or user instructions specify.

## Step 6: Complete

After the commit, complete the item per the completion rule in agent-context.

## Report

Report:
- Files changed and lines added
- Test names and count
- Gate results
- Commit SHAs
