# Block Grounding Rules

Shared by `/wipnote:plan` and `/wipnote:visual-plan`.

Run `wipnote plan blocks` first. It lists the supported block types and their required fields. Never hardcode them.

A block is grounded when every field, route, and file path it names already exists, or will exist as a direct output of the slice. Never invent schema, routes, or files. If a block type cannot be grounded, skip it and say why in `decisions_notes`.

- `data-model` rows: real or will-exist field names and types.
- `data-model` for a route: put the method and path in `name` (e.g. `name: POST /api/ingest — 429 response`). A data-model's `title` is not rendered. The route must be one the slice implements.
- `file-tree` entries: real files the slice touches.
- `wireframe` HTML: `var(--wf-*)` design tokens only, never raw hex or rgb. The renderer injects the palette, so raw colors render broken.

```html
<div style="background: var(--wf-bg); color: var(--wf-text); border: 1px solid var(--wf-border);">
  content
</div>
```

If the slice does not justify a block, omit it. Do not pad with speculative blocks.
