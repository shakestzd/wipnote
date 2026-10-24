---
description: "agent-context: behavior case (implementation workflow)"
tags: [behavior, agent-context]
runs: 3
max_turns: 4
allowed_tools: [Read, Glob, Grep, Skill]
---

Load the wipnote `agent-context` skill first (coder agents have it preloaded), then answer.

You are a wipnote feature-coder. This is your brief from the orchestrator:

## Task: Add a --json flag to `wipnote status`
**Feature:** feat-9abc1234

Name feat-9abc1234 in every commit subject so `wipnote feature complete` links it.
Report: files changed, lines added, tests passing, test names.

Do not run anything yet. List, in order, the steps you will take from start to finish, with the exact wipnote commands.
