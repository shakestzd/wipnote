---
name: wipnote:visual-plan
description: Adds or revises grounded visual blocks (data-model, file-tree, wireframe, diagram) on an existing wipnote plan and regenerates its HTML. Use for legacy plans or later block changes, not for creating a new plan.
disable-model-invocation: true
---

# wipnote Visual Plan (after-the-fact enrichment)

New plans get blocks inline during the `wipnote:plan` interview. Use this skill only to add or revise blocks on an EXISTING plan, such as a legacy plan or a slice that needs a block changed later. To create a plan, use `wipnote:plan` instead.

**Trigger keywords:** enrich existing plan, add visual blocks to existing plan, add blocks to
a plan that already exists, revise blocks on a plan, backfill blocks on a legacy plan,
visualize an existing plan slice, add wireframe to existing plan, add data model to existing
plan, add api endpoint to existing plan, add file tree to existing plan

---

## Step 1: Read the live block catalog

```bash
wipnote plan blocks
```

It lists the supported block types, their required fields, and their row keys. Never hardcode these; the catalog changes.

---

## Step 2: Identify the plan and target slices

| User says | Target |
|-----------|--------|
| `enrich plan plan-<id>` | Plan ID |
| `add blocks to slice <N> of plan-<id>` | Specific slice |
| `add a data model to this plan` | Current/most recent open plan |

If the plan or slice is unclear, list plans and ask the user to confirm:

```bash
wipnote plan list-yaml
```

---

## Step 3: Read the current plan slices

Read `.wipnote/plans/<plan-id>.yaml` directly (read-only). For each target slice, note:
- the title and goal
- files and entities already mentioned
- acceptance criteria (these decide which blocks are relevant)

---

## Step 4: Author grounded blocks for each slice

Follow the grounding rules in [`${CLAUDE_PLUGIN_ROOT}/skills/plan/BLOCK_GROUNDING.md`](../plan/BLOCK_GROUNDING.md). Use only the block types and fields from Step 1.

---

## Step 5: Rewrite the plan YAML with blocks

Copy the plan YAML, add the `blocks:` entries to the target slices, and write it back. Keep all other fields and their order.

```bash
cp .wipnote/plans/<plan-id>.yaml /tmp/<plan-id>.yaml
# edit /tmp/<plan-id>.yaml
wipnote plan rewrite-yaml <plan-id> --file /tmp/<plan-id>.yaml
```

Never write `.wipnote/plans/` directly; writes go through `rewrite-yaml`.

---

## Step 6: Regenerate the plan HTML

```bash
wipnote plan render <plan-id>
```

If it fails, report the error verbatim. Do not build the artifact by hand.

---

## Step 7: Report and link to the dashboard

```markdown
## Visual Plan: <plan-id>

**Plan:** `<plan-id>` — <title>
**Dashboard:** `wipnote serve` then open http://127.0.0.1:8080 (or http://127.0.0.1:8088 in devcontainer)

### Blocks added
<bullet list: slice N — block type — key fields, grounded in the real slice content>

### Next step
Open the dashboard to review the rendered blocks. Re-run this skill to enrich more slices.
```

Port 8080 is the default; use 8088 in the devcontainer. Tell the user which applies.
