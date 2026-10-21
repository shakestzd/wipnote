# Research-first gate — design note

Status: implemented · Date: 2026-10-21

## Problem

Agents design new behaviour from the first familiar pattern. Near miss: a
"hamburger menu" for header toolbar actions, when the documented pattern is a
priority overflow into a labelled "More actions" menu with the primary action
kept visible (Polaris, Primer ActionBar, SAP Fiori, Carbon). The NN/g hamburger
data is about site navigation, not actions. Nothing in wipnote asked for prior
art before this work started.

## What exists (read from code, not guessed)

| Piece | Where | Gap |
|---|---|---|
| Dependency research gate | `checkDependencyResearchCompleteGate` (`cmd/wipnote/workitem.go`) | Fires only when a dependency manifest changed. Stores URLs/waiver as a free-text `research-evidence:` content note. |
| Commit-time dependency guard | `core/hooks/commit_research_guard.go` | Dependencies only. Session-state based. |
| v4 plan research gate | `plan/planyaml/validate.go` (`design.research`, per-slice `research`/`research_waiver`) | Plans only. Nothing covers ad-hoc features, bugs or spikes. |
| Spikes | generic `wiCreateCmd`; `SpikeCollection` | No link back to the item they research. Findings are prose in `Content`. |
| `wipnote relevant` | `cmd/wipnote/relevant.go` | Finds existing work. Nothing records that it was run. |
| Node properties | `core/htmlparse/node_props.go` | Non-string values round-trip losslessly as JSON in `data-node-props`. |
| Config | `.wipnote/config.json`, per-feature readers (`ReadSpecEnforcement`) | No `research` section. |
| Dispatch hook | `PreToolUse` (`core/hooks/pretooluse.go`) | No check on `Task`/`Agent` dispatch. |

## What changes

1. **Research record** stored in the work item's `research` property, a JSON
   object in `data-node-props`. It holds the question, sources (url, type,
   retrieved), findings, exists (or `none found` plus the queries used),
   options, chosen, rejected (with reasons), gaps and waiver. No new HTML
   section, so the writer's section-preservation list is untouched. An artifact
   with no `research` property loads as before. `wipnote <kind> show` and
   `wipnote lineage` render the record.
2. **Spikes are the vehicle.** `wipnote research start "<q>" --for <id>`
   creates a spike that is `spawned_from` the item and carries `research_for`.
   The researcher fills the spike's record with `wipnote spike research add`.
   `wipnote spike complete` copies that record onto the parent.
3. **Change kind** (`change_kind` property): `design`, `ui`, `capability` and
   `dependency` are gated; `bugfix`, `chore`, `docs` and `rename` are not. It is
   set with `--change-kind` at create time, or inferred from title keywords with
   a printed note. Bugs default to `bugfix`. Spikes have no kind; they are the
   research. Items created before this change have no kind and are not gated.
4. **Config**: `.wipnote/config.json` → `{"research": {"mode": "warn",
   "required_kinds": [...]}}`. Mode is `off`, `warn` (the default) or `block`.
5. **Enforcement points**:
   - `<kind> start`: stderr warning (never fails).
   - `PreToolUse` on `Task`/`Agent` dispatching a `*-coder` subagent: advisory
     in warn mode, deny in block mode.
   - `check --gate --work-item` and `<kind> complete`: fail in block mode
     unless the record is complete or waived.
   - `--research-waiver` now satisfies both the kind gate and the dependency
     gate, and is written into the record.
6. **The dependency path is unchanged**: it still hard-blocks on a manifest
   change. `--research-url` URLs are now also stored as primary sources in the
   record. The legacy note is still written.

The rule text lives in one Go constant (`core/research`). `wipnote research
--help`, the gate messages and the hook all print it. Skills and the system
prompt link to that command rather than restating the rule.

## Alternatives rejected

| Alternative | Why rejected |
|---|---|
| A new `research` work-item type | The brief says to reuse spikes. A parallel object would split lineage. |
| A new `<section class="research">` in the HTML | Needs writer and parser changes plus an addition to `preserveSupplementalSections`. The JSON property path already round-trips (bug-c65a5f4e). |
| Labels/tags for gating | `Node` has no wired labels (`CapabilityTags` is dead). A single typed `change_kind` property is simpler to validate. |
| Defaulting to block | That would break existing projects on upgrade. The brief asks for warn. |
| Running `wipnote relevant` automatically inside the gate | That is a non-goal: the gate makes no network calls and no searches. `exists` is the agent's recorded result. |
| Gating every Task dispatch | Research and exploration agents must stay free, since they are how the record gets filled. Only `*-coder` (implementer) dispatches are checked. |

## Borrowed patterns (sources)

- Record shape (problem statement, considered options, decision outcome, pros
  and cons per option) follows MADR 4.0.0: <https://adr.github.io/madr/>
  (retrieved 2026-10-21, primary).
- `off | warn | block` severity, where warn does not affect the exit code,
  mirrors ESLint rule severities:
  <https://eslint.org/docs/latest/use/configure/rules> (retrieved 2026-10-21,
  primary).
- Hook advisory and deny use Claude Code PreToolUse
  `hookSpecificOutput.additionalContext` and `permissionDecision: "deny"`:
  <https://code.claude.com/docs/en/hooks> (retrieved 2026-10-21, primary).

Evidence gap: the hooks page fetch was truncated, so it did not confirm the
subagent-dispatch `tool_input` field names. `subagent_type` and `prompt` are
taken from wipnote's existing Task handling, not from upstream docs.
