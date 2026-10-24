# Enrich an existing plan with visual blocks

New plans get blocks during the draft interview. Use this file only to add or revise blocks on a plan that already exists: a legacy plan, or a slice whose block must change later.

## Checklist

Copy this checklist and tick it off:

```
Enrich plan (plan-id):
- [ ] 1. Live block catalog read
- [ ] 2. Plan and target slices confirmed
- [ ] 3. Target slices read
- [ ] 4. Grounded blocks authored
- [ ] 5. YAML rewritten and validate-yaml clean
- [ ] 6. HTML rendered
- [ ] 7. Reported with the dashboard link
```

## Step 1: Read the live block catalog

```bash
wipnote plan blocks
```

It lists the supported block types, their required fields, and their row keys. Never hardcode these; the catalog changes.

## Step 2: Confirm the plan and target slices

| User says | Target |
|---|---|
| `enrich plan plan-<id>` | Every slice in the plan |
| `add blocks to slice <N> of plan-<id>` | That slice |
| `add a data model to this plan` | The current or most recent open plan |

If the plan or slice is unclear, run `wipnote plan list-yaml` and ask the user to confirm.

## Step 3: Read the target slices

Read `.wipnote/plans/<plan-id>.yaml` (read-only). For each target slice, note its title and goal, the files and entities it names, and its acceptance criteria; the criteria decide which blocks are relevant.

## Step 4: Author grounded blocks

Follow [BLOCK_GROUNDING.md](BLOCK_GROUNDING.md). Use only the block types and fields from Step 1.

## Step 5: Rewrite and validate

```bash
cp .wipnote/plans/<plan-id>.yaml /tmp/<plan-id>.yaml
# add the blocks: entries to the target slices in /tmp/<plan-id>.yaml; keep every other field and its order
wipnote plan rewrite-yaml <plan-id> --file /tmp/<plan-id>.yaml
wipnote plan validate-yaml <plan-id>
```

Never write `.wipnote/plans/` directly. If `rewrite-yaml` or `validate-yaml` reports an error, fix `/tmp/<plan-id>.yaml` and run both again. Continue only when validation is clean.

## Step 6: Render the plan HTML

```bash
wipnote plan render <plan-id>
```

If it fails, report the error verbatim. Do not build the artifact by hand.

## Step 7: Report

```markdown
## Visual Plan: <plan-id>

**Plan:** `<plan-id>` — <title>
**Dashboard:** `wipnote serve`, then http://127.0.0.1:8080 (http://127.0.0.1:8088 in the devcontainer)

### Blocks added
<one bullet per block: slice N — block type — key fields>
```
