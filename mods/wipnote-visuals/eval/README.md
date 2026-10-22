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

## Lint false-positive check (2026-10-21, offline, no model calls)

`bun eval/lintcheck.ts <transcript.jsonl>` runs `lintReply` (strict) over the 42 saved eval replies and the
assistant text blocks of a Claude Code session transcript (replies under 25 words skipped). The transcript
was this feature's own working session (78 replies, mostly orchestration reports and design prose).

| rule | eval hits | eval replies hit (of 42) | rate / reply | transcript hits | transcript replies hit (of 78) | rate / reply |
|---|---|---|---|---|---|---|
| long | 16 | 12 | 0.38 | 10 | 8 | 0.13 |
| negclose | 0 | 0 | 0 | 3 | 3 | 0.04 |
| vocab | 0 | 0 | 0 | 2 | 2 | 0.03 |
| punct | 0 | 0 | 0 | 1 | 1 | 0.01 |
| opener | 0 | 0 | 0 | 1 | 1 | 0.01 |
| filler, emdash | 0 | 0 | 0 | 0 | 0 | 0 |

Sample hits and verdicts (real tell, or false positive = FP):

| rule | hit | verdict |
|---|---|---|
| long | eval compare: "Choose Postgres only if you expect heavy concurrent writes from many…" (26+ words) | real, low value: one clear sentence that is long |
| long | eval rebase x3: "It takes the commits on your current branch that aren't on the target…" | real; explanatory prose runs long by nature |
| long | eval hooks: "The hook receives a JSON payload describing the event on stdin, a…" | real |
| long | transcript: `3. **Close the loop cheaply.** Only when a check failed, add one line…` | FP-ish: the splitter cuts at the `.` inside the bold label and counts the rest as one sentence with the 20-word list limit |
| long | transcript: `**Next:** do you want the live test with …` | real, but a question with options; the limit is harsh on it |
| vocab | transcript: `"…, showcasing…"` inside a list of banned words | FP: quoting the rule itself |
| vocab | transcript: `It left out "crucial" and the em dashes.` | FP: quoting the lint output |
| negclose | transcript: "It is not allowed to edit repo files." | FP: a factual constraint |
| negclose | transcript: "If it is not, the mod is not loaded." | FP: an instruction |
| negclose | transcript: "That does not affect the Receipt." | borderline: a plain fact, not a rhetorical closer |
| punct | transcript: "human–agent" | real per the rule (en dash between words); harmless |
| opener | transcript: "It … It … It …" three sentences | real, mild |

What this says:

1. **Clean replies really are clean.** 0 hits for filler and emdash anywhere, 0 vocab/opener/punct on the 42 eval
   replies. A failure-only note therefore costs nothing on most turns.
2. **`long` is the noisy rule.** 0.38 hits per eval reply, almost all real by the letter of the 25-word rule but of
   low value, plus list items with a bold label that the sentence splitter cuts wrong. Not for default feedback.
3. **`negclose` is 3 for 3 false positives** on technical prose: short negated facts are normal here. Strict only,
   and a candidate to drop. The "two `negclose` hits under the style" noted after run 1 cannot be re-checked:
   run 2 overwrote `eval/out` and the current 42 replies have no `negclose` hit.
4. **`vocab` hits were both meta** (replies discussing the lint). In normal use the word list is the clearest tell
   and should stay first in the ranking.

Default recommendation: `/plainspeak off` stays the default; `80` (vocab, filler, emdash, long) is the mode to try
first because it drops `negclose`, `punct` and `opener`. Proposed rule changes from this pass: (a) treat a list
item's `**label.**` as a label, not a sentence end, before counting; (b) narrow `negclose` to a paragraph whose
previous sentence is affirmative and the closer is 6 words or fewer; (c) raise `long` to 30 words for prose that
explains a command or concept, or report it only when 3 or more sentences in one reply are long.

### After fixes (a) and (b), same replies

(a) and (b) were made; (c) was not (the user's call). Same 42 eval replies; the transcript had grown to 82
replies, so both columns below were re-run on those 82 with the old and the new rules.

| rule | eval before | eval after | transcript before (82) | transcript after (82) |
|---|---|---|---|---|
| long | 16 hits / 12 replies | 13 / 10 | 10 / 8 | 4 / 4 |
| negclose | 0 | 0 | 3 / 3 | 0 |
| vocab | 0 | 0 | 2 / 2 (both meta) | 2 / 2 |
| opener | 0 | 0 | 2 / 2 | 2 / 2 |
| punct | 0 | 0 | 1 / 1 | 1 / 1 |
| filler, emdash | 0 | 0 | 0 | 0 |

The six `long` hits that went away on the transcript were all list items with a bold label, now judged as one
prose sentence with the 25-word limit; the three on the eval replies likewise. The three `negclose` hits were the
false positives listed above; the tests in `hooks/lint.test.ts` pin them, and pin a real closer that still fires
("The old parser guessed at the encoding. The new one does not."). The remaining `long` hits are real by the
25-word rule.

## Run 2: rule reworded (2026-10-21, sonnet, n=3 per cell, 42 valid replies)

Rule change: "for steps use `wip:steps` **instead of a numbered list**", "say each thing once: never repeat a block in prose", and "use sparingly" dropped. 78 tokens (was ~95).

| Check | `rule` only | `rule` + Plain English style |
|---|---|---|
| steps prompt gives a `wip:steps` block | **3/3** (was 1/1) | **3/3** (was 0/2) |
| compare prompt gives `wip:compare` | 3/3 | 3/3 |
| callout prompt gives `wip:callout` | 3/3 | 3/3 |
| Steps repeated as prose after a block | 0/3 | 0/3 |
| Emitted fences that failed to parse | 0 | 0 |
| Steps marked done (`x `) | 0 of 40 | 0 of 50 |
| Average words / lint tells per reply | 224 / 0.4 | 204 / 0.3 |

Follow-ups:

1. **Style/steps conflict fixed.** It was solved in the mod's rule, not in the style.
2. **Extra blocks on top of the asked-for one.** The callout prompt (force-push) got an added `compare` or `steps` block in 5/6 replies; the compare prompt got an added callout in 3/6. Dropping "use sparingly" probably caused this. Next try: add "Use only what the answer needs." (about 8 tokens, 86 total) and re-run.
3. **Stray blocks on explanation prompts.** "What does git rebase do?" got all three blocks once in 6 (rule only, none under the style); the Go debugging question got a steps block once in 6. "Mutex vs channel" got `compare` in 5/6, which is arguably right for a "difference between" question, so treat it as a weak prompt, not a failure.
4. **Style helps a little:** fewer words (204 vs 224), fewer long sentences (5 vs 8).
