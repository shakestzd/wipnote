# Archived Skills

This directory contains skills that have been consolidated or deprecated.

## parallel-orchestrator (Archived 2025-12-31)

**Reason:** Consolidated into `erinn-tracker` skill.

**Why?** The parallel-orchestrator skill provided orchestration and delegation patterns, but this created confusion:
- Two skills with overlapping responsibilities
- Users didn't know which to activate
- Orchestration is a core part of the Erinn AI workflow, not a separate concern

**Solution:** All orchestration directives are now in `erinn-tracker` as "Orchestrator Mode" (Section 1 of Core Responsibilities).

**Migration:** The `erinn-tracker` skill now includes:
- Orchestrator mode directives (delegation patterns)
- 6-phase parallel workflow
- Task ID coordination helpers
- All orchestration anti-patterns and best practices

**What was moved:**
- Delegation philosophy and decision framework
- Git delegation requirements (ALWAYS DELEGATE)
- Parallel workflow coordination
- Task ID pattern for result retrieval
- SDK orchestration methods

**See:** `erinn-tracker/SKILL.md` for the consolidated skill.
