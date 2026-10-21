# wipnote-visuals (dev mod)

Draws three fenced blocks in replies as visuals. The model sends only the short inputs; without the mod each block still reads as plain text. A rule under 100 tokens (`prompt.compose`) tells the model when to use them. `/visuals on|off` toggles drawing and the rule.

**wip:steps** — one step per line; `x ` = done, `> ` = current, no prefix = todo.

````
```wip:steps
x Scaffold the mod
> Write the parsers
Add tests
```
````

**wip:compare** — `# Option` starts an option (a trailing `*` marks the recommended one), then `+ pro` and `- con` lines.

````
```wip:compare
# SQLite*
+ no server
- single writer
# Postgres
+ concurrent writes
- extra service
```
````

**wip:callout** — first line `decision:`, `warning:` or `note:` then text; further lines continue it.

````
```wip:callout
decision: Use SQLite
Simplest option that meets the load.
```
````

Open or malformed fences stay plain text. Parsers are in `hooks/blocks.ts` (pure), drawing in `hooks/components.tsx`.
