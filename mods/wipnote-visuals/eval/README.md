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
