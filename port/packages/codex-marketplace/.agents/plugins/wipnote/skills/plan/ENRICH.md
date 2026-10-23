# Enrich an existing plan with visual blocks

New plans get blocks during the draft interview. Use this file only to add or revise blocks on a plan that already exists: a legacy plan, or a slice whose block must change.

```
Enrich plan (plan-id):
- [ ] 1. Live block catalog read
- [ ] 2. Target slices confirmed and read
- [ ] 3. Grounded blocks authored
- [ ] 4. YAML rewritten; validate-yaml clean
- [ ] 5. HTML rendered and reported
```

1. **Read the catalog.** `wipnote plan blocks` lists the block types, their required fields and row keys. Never hardcode them.
2. **Confirm the targets.** "enrich plan-<id>" means every slice; "add blocks to slice N" means that slice. If the plan is unclear, run `wipnote plan list-yaml` and ask. Read `.wipnote/plans/<plan-id>.yaml` and note each target slice's goal, files, entities and acceptance criteria; the criteria decide which blocks matter.
3. **Author grounded blocks** per the Blocks rules in [SKILL.md](SKILL.md): every field, route and path must exist or be a direct output of the slice, and wireframes use `var(--wf-*)` tokens only.
4. **Rewrite and validate.** Copy the YAML to a temp file, add the `blocks:` entries (keep every other field and its order), then run `wipnote plan rewrite-yaml <plan-id> --file <tmp>` and `wipnote plan validate-yaml <plan-id>`. Never write `.wipnote/plans/` directly. Repeat until validation is clean.
5. **Render and report.** `wipnote plan render <plan-id>` writes the plan HTML; report its path and one line per block added (slice, type, key fields). If rendering fails, report the error verbatim rather than building the HTML by hand.
