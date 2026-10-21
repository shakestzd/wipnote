# wipnote-visuals live eval

Checks, against a real model, whether the `RULE` in `hooks/blocks.ts` makes it emit `wip:*` blocks
where they help and nowhere else. 7 prompts (3 that suit a block, 4 that do not) x 2 conditions
(`rule`, `rulestyle` = rule plus the Plain English style) x N reps.

```bash
eval/run.sh 2          # needs `claude` and bun; about 15 s per reply
bun eval/analyze.ts    # fences emitted, parse success, steps marked done, lint tells
```

Gotchas found the hard way:

- **Close stdin** (`< /dev/null`) on every `claude -p` inside a `while read` loop. Otherwise it swallows the
  loop's input and each reply also answers the other prompts, which looks exactly like a model that
  over-uses the blocks.
- **Do not run replies in parallel from one directory.** Run them one at a time, each in its own directory.
- In headless mode the mod's hooks module can fail to load ("rollout switch" message), so the rule is
  injected with `--append-system-prompt` here instead of relying on `--plugin-dir`.
- Prompts that invite a tool call ("look at my .wipnote directory") hit `--max-turns 1`. Keep prompts tool-free.

## Results (2026-10-21, sonnet, n=2 per cell, 25 valid replies; 3 hit `--max-turns 1` and are excluded)

| Check | `rule` only | `rule` + Plain English style |
|---|---|---|
| Block emitted when it suits (steps / compare / callout) | steps 1/1, compare 2/2, callout 1/1 | steps **0/2**, compare 1/1, callout 2/2 |
| Stray blocks on 8 no-visual prompts per condition | **0/8** | **0/8** |
| Emitted fences that failed to parse | 0 | 0 |
| Steps marked done (`x `) when nothing had run | 0 of 6 | 0 of 0 |
| Average words / lint tells per reply | 243 / 0.9 | 225 / 0.4 |

What to act on:

1. **The rule does not over-trigger.** An earlier run showed all three blocks on every reply; that was the stdin bug below, not the model.
2. **The style competes with `wip:steps`.** Its "numbered lists for sequences" line wins, so steps come out as prose (0/2). Fix: one clause in the style, or in the rule, saying a step list is a `wip:steps` block when the mod is loaded.
3. **"Do not repeat them in prose" is not honoured.** After a `wip:steps` block the reply often restates the steps as numbered prose. The rule is at ~95 of its 100 tokens, so shorten something else to make room for a stronger line.
4. **The style helps the lint numbers a little** (long sentences 12 -> 3), but `negclose` fired twice under it. Check those two replies before turning the writing check on by default.
5. n=2 is small. Raise `reps` before changing the rule on this evidence.
