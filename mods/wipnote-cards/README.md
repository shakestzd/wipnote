# wipnote-cards (dev mod)

A Claude Code mod that shows wipnote state for few model tokens.

- **Status line** (no model tokens): `wipnote · <id> · <status> · 3/5 steps` for the
  work item the session has claimed. It refreshes at session start and after each turn.
- **Cards** (about 10 tokens): an assistant message that contains a fenced block with the
  info string `wipnote-ref` and a work-item id draws as a card: title, coloured status,
  step bar. The saved message keeps only the short block.

The rule that asks the model for the block is sent only while this mod is loaded.

## Run it

```bash
claude --plugin-dir mods/wipnote-cards
```

Mods draw only where Claude Code itself draws: a local terminal or a local desktop
session. A cloud session viewed in the Claude app has no drawing surface, so the block
shows as plain code there.

## Check it

```bash
claude plugin validate mods/wipnote-cards
claude plugin test mods/wipnote-cards
```

It is not yet part of the generated plugin tree; shipping mods needs `build-ports` support.
