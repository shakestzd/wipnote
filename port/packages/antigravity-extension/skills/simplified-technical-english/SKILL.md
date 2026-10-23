---
name: simplified-technical-english
description: Write, rewrite or review text in ASD-STE100 Simplified Technical English (Issue 9). Use this skill whenever the user asks for STE, "Simplified English", "controlled English", "plain technical English", maintenance-style instructions, a step-by-step procedure, a warning or caution, or descriptive technical text that non-native readers must understand. Also use it to check a draft for STE problems, even when the user does not name the standard. It drafts first, converts last, runs a linter, checks words against a local dictionary lookup, and labels the output honestly. It never claims compliance.
disable-model-invocation: true
---

# Simplified Technical English (ASD-STE100, Issue 9)

STE is a controlled language for technical writing. It limits vocabulary, grammar and sentence length so that
any reader, including a non-native one, reads one meaning. This skill helps you write STE-style text. It does not
certify anything: ASD has not checked this skill, and no tool can prove compliance.

Rule numbers below (for example Rule 5.3) point to the spec. Plain summaries are in
`references/rules-summary.md`. Read the spec itself for the exact wording. Limits live in one place: run
`uv run scripts/ste_lint.py --show-limits` and use those values when you draft.

**Paths.** `scripts/` and `references/` are folders beside this SKILL.md. On Claude Code that folder is
`${CLAUDE_SKILL_DIR}`, so run `uv run ${CLAUDE_SKILL_DIR}/scripts/ste_lint.py`. On other harnesses, use the
folder that this SKILL.md was loaded from. The commands below show the short `scripts/...` form.

## Workflow

Copy this checklist and tick it off:

```
STE pass:
- [ ] 1. Each part classified: procedure, descriptive or safety
- [ ] 2. Content drafted in plain good English
- [ ] 3. Converted to STE, meaning kept
- [ ] 4. Linter run; every finding fixed or justified
- [ ] 5. Words checked with lookup (or the gap stated)
- [ ] 6. Disclosure line and checks-run line added
```

Follow these steps in order. The order matters, and the reason for each step is given.

1. **Classify the text.** Decide whether each part is a procedure, descriptive text or a safety instruction.
   The rules differ by type, so a wrong label gives wrong limits and wrong grammar.
2. **Write the content normally first.** Get facts, order and completeness right in plain good English.
   Do not think about STE yet. Content comes first because a fact that is wrong in STE is still wrong
   (the reasoning is in `references/evidence-notes.md`).
3. **Convert to STE as a final pass.** Rewrite sentence by sentence using the handling below. Change form, not
   facts. If a conversion would change the meaning, keep the meaning and flag the sentence.
4. **Run the linter.** Save the text to a file and run `uv run scripts/ste_lint.py FILE` (or pipe it on stdin).
   Use `--mode procedure` for text that is all steps. Each finding is `line:col  rule  message`. Fix every
   finding or decide, with a reason, that it is a false alarm (hints are marked "Hint"). The linter
   machine-checks only a subset, so a clean run is not a pass.
5. **Check the words against the dictionary.** Run `uv run scripts/lookup.py WORD[:POS] ...`, or pipe text on
   stdin. `--help` holds the exact output contract; read it there. Act on each status:
   - *approved*: keep the word, and check the sentence uses its approved part of speech and meaning (Rules 1.2,
     1.3). Lookup prints no meanings, so read the spec entry when unsure.
   - *unapproved*: use an alternative shown after `->`. If the part of speech differs or the sentence reads
     badly, rebuild the sentence (Rule 9.1).
   - *check-pos*: decide which sense the sentence uses, then rerun with a hint such as `word:n`, because the
     hint keeps only the sense the sentence needs.
   - *not-found*: treat the word as a technical noun or verb candidate (Rules 1.5, 1.6, 1.12) and flag it for a
     person. Never invent an approval.
   - Lookup does not know the sentence's part of speech, so a correct technical noun can show *unapproved* with
     a verb sense (seen with "filter" and "screw"). Rerun with `word:n` before rewriting. If the noun sense is
     not-found, flag it for review; that is not an error. Never rewrite a word only because the unhinted lookup
     flagged it.
   - Exit code 1 means something needs a look, not that the text is wrong. Stdin skips numbers, part-number
     tokens and all-uppercase identifiers; the `skipped: N` line counts them, so check them by hand when they matter.
   - If the database is missing (exit 2), tell the user how to build it (`README.md`), say the dictionary was
     not checked, and carry that gap into the label.
6. **Label the result.** End STE output with the disclosure line below, then one line saying which checks ran
   and which did not (for example: "Linter run, 0 findings. Dictionary lookup not run."). Do not write
   "compliant", "certified" or "approved".

Disclosure line (the exact wording lives in `scripts/ste_lint.py`; print it with `--print-disclosure`):

> AI-assisted draft in the style of ASD-STE100. Not checked or endorsed by ASD. Review by a person is required.

## Handling by text type

**Procedures (Section 5).** Write each step as a command (Rule 5.3), one instruction per sentence unless two
actions happen together (Rule 5.2). Put a condition the reader must know first at the start, divided from the
command by a comma (Rule 5.4). Number the steps. Keep each sentence inside the procedure limit (Rule 5.1). A
note gives information only, never an instruction or a limit (Rule 5.5).

**Descriptive text (Section 6).** Use no commands. Give information gradually, one subject per sentence
(Rule 6.1). Open each paragraph with a topic sentence, keep one topic per paragraph, and keep paragraphs short
(Rules 6.4 to 6.6). Link sentences with connecting words (Rule 4.4). Use the active voice; the passive is
allowed only when the agent is unknown (Rule 3.6).

**Safety instructions (Section 7).** Put the warning or caution before the step it protects. Mark the level
with a signal word (Rule 7.1). Start with a clear command or condition (Rule 7.2). State the risk or result
(Rule 7.3). Use a warning when people can be hurt and a caution when equipment can be damaged. When both
apply, use a warning.

**Across all text.** Use the simple tenses only (Rule 3.2), no contractions (Rule 4.2), no semicolon
(Rule 8.1), articles where English needs them (Rule 4.5), and the same word for the same thing every time
(Rules 1.11, 9.4). Prefer a verb to a noun for an action (Rule 3.7). Avoid phrasal verbs (Rule 9.3).

## Technical nouns and verbs

The dictionary has no technical nouns or technical verbs, because they depend on the subject field.

- A word outside the dictionary is allowed only if it is a technical noun (Rules 1.5, 1.6) or a technical verb
  (Rule 1.12). Technical verbs follow the same grammar rules as other verbs.
- Prefer terms the user supplies: a company glossary, a parts list, a label, a standard. Ask for one when the
  text has many unfamiliar terms.
- Use one term per item (Rule 1.11). Do not turn a technical noun into a verb or a technical verb into a noun
  (Rules 1.7, 1.13). Choose short, plain terms over regional words, slang or jargon (Rules 1.9, 1.10).
- A term that is not in the dictionary and that you cannot place in a technical category: **flag it for a
  person** in a short list ("Terms needing review") with the sentence where it appears. Do not invent a
  meaning, an approval or a category.

## Reviewing a draft

When asked only to review, run steps 4 and 5, then group the findings by rule number. Say which findings the
linter found and which you judged by reading. Do not rewrite unless asked. State that the linter and your
reading are partial.

## AI-use statement

ASD's white paper says AI can support STE writing but cannot replace human oversight, that automated checks
vary in accuracy, and that AI-assisted content should be disclosed. So: always add the disclosure line, always
send the text to a human reviewer, report the limits of the checks you ran, and never present your output as
verified STE.

## Licence

The ASD-STE100 specification is copyrighted, and its owner bars reproduction without written authority. This
skill holds no spec text and no word list. Do not quote the spec or the dictionary in your output. When the
user wants the exact rule wording or the full dictionary, point to their own copy of the spec.

## Files

- `scripts/ste_lint.py` - linter for the mechanical rules, `--show-limits`, `--print-disclosure`.
- `scripts/build_dictionary.py`, `scripts/lookup.py` - build and query the local dictionary from the user's own PDF.
- `references/rules-summary.md` - all rules, in original words, marked S (script), D (dictionary), J (judgement).
- `references/evidence-notes.md` - what is verified, the evidence behind the workflow, the white paper, the licence.
- `README.md` - setup and tests.
