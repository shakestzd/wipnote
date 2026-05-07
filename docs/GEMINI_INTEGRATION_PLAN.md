# Gemini CLI Integration Plan for wipnote

This document outlines the research findings and implementation strategy for making the wipnote ecosystem work seamlessly with the Gemini CLI.

## Research Summary

### Current State
*   **wipnote CLI:** A mature Go binary (`wipnote`) that manages work items (features, bugs, spikes) as HTML files. It provides robust APIs for session tracking, planning, and analytics.
*   **Plugin Ecosystem:** The Claude Code plugin is the "gold standard" here, using Markdown files for slash commands and specialized agent definitions.
*   **Gemini CLI Integration:** Currently exists as a blueprint in `GEMINI.md`, but the implementation files in `packages/gemini-extension/` are missing.

### Key Integration Points
*   **Skills:** Gemini CLI uses `SKILL.md` files for procedural instructions and workflows.
*   **Slash Commands:** Gemini CLI uses TOML files (e.g., `commands/wipnote/start.toml`) to define interactive commands.
*   **Safety & Attribution:** The `wipnote agent-init` command provides the necessary safety rules and attribution logic that must be injected into the Gemini session.

---

## Implementation Plan

### Step 1: Bootstrap the Extension Structure
Create the core directory structure for the Gemini extension to ensure auto-discovery by the Gemini CLI.
*   **Path:** `packages/gemini-extension/`
*   **Files:**
    *   `gemini-extension.json`: Extension manifest.
    *   `skills/wipnote/SKILL.md`: Core instructions, safety rules, and work attribution logic.
    *   `commands/wipnote/`: Directory for slash command TOML files.

### Step 2: Implement Slash Commands
Translate the 6 core Claude Code commands into Gemini-native TOML format. These commands will use the `!{wipnote ...}` syntax to bridge the Gemini interface with the Go CLI.
1.  `/wipnote:start`: Initialize session and show project overview.
2.  `/wipnote:status`: Check current work item and project status.
3.  `/wipnote:plan`: Smart planning workflow for new initiatives.
4.  `/wipnote:spike`: Create a research spike for investigations.
5.  `/wipnote:recommend`: Get strategic recommendations from analytics.
6.  `/wipnote:end`: End session with a summary of work completed.

### Step 3: Define the Gemini Orchestrator Skill
Adapt the `orchestrator-directives-skill` specifically for the Gemini CLI context.
*   Integrate Gemini-specific sub-agents (`codebase_investigator`, `generalist`) into the `wipnote` workflow.
*   Enforce "research-first" discipline and cost-optimized model selection (e.g., using Gemini for exploration).

### Step 4: Update Project Documentation
Ensure all documentation is synchronized and provides clear installation paths.
*   Update `GEMINI.md` to reflect the actual implementation.
*   Add a "One-Liner" setup guide: `gemini skills link ./packages/gemini-extension --scope workspace`.

### Step 5: Validation & Quality Gates
Verify the integration through end-to-end testing.
*   Confirm Gemini CLI discovers and activates the `wipnote` skill.
*   Validate that slash commands correctly execute the underlying `wipnote` CLI and parse its output.
*   Ensure safety rules from `agent-init` are respected during the session.

---

## Success Criteria
*   [ ] `packages/gemini-extension/` exists and is populated.
*   [ ] `/wipnote:start` successfully fetches project summary in Gemini.
*   [ ] Gemini CLI auto-activates the `wipnote` skill upon project entry.
*   [ ] Work items created in Gemini are visible in the `wipnote serve` dashboard.
