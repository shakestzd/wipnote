# Evidence notes and sources

## What is verified and what is not

- **Limits.** Each numeric limit the linter uses was read from the Issue 9 text of the spec, with its rule number. The research notes gave four secondary figures for sentence, paragraph and noun-cluster limits. All four match the spec (Rules 5.1, 6.3, 6.6 and 2.1). The notes missed the limit for notes (Rule 5.5), which the spec gives as the descriptive limit. The spec gives no separate limit for safety text; the linter applies the procedure limit as an assumption and says so.
- **Dictionary size.** The spec's introduction states a count of approved and of unapproved words. `build_dictionary.py` prints its own parse count. Compare the two when you build, and treat a gap as a parser limit, not as a fact about the standard.
- **No benchmark.** There is no peer-reviewed study of language models writing STE. The linter has been tested on synthetic inputs only, not against a corpus of expert-written STE.

## Why the workflow drafts first and converts last

One informal blog test (few tasks, a model as judge, a single model) found that asking for STE in the first prompt gave worse answers than a plain prompt. This is weak evidence: it is small, judged by a model and not peer reviewed. It fits a plausible mechanism, which is that a style constraint competes with reasoning. The skill follows the cautious reading: get the content right first, then convert.

## ASD white paper on STE and AI

ASD's Simplified Technical English Maintenance Group states that AI can support STE writing but cannot replace human oversight. It says automated STE compliance checks currently vary in accuracy, and that responsibility stays with the human author. It asks for disclosure of AI use and for data, validation and quality-assurance policies. It names no approved tool and endorses none. The skill's footer line, its "subset only" linter footer and its refusal to claim compliance follow from this.

## Licence

The specification carries a copyright notice that bars reproducing it, in whole or in part, without ASD's written authority. This skill therefore holds no spec text, no example sentences and no word lists. Rules are cited by number and summarised in original words. The dictionary lookup is built on the user's own copy of the spec and stays on the user's machine.

## Sources

- ASD-STE100 Simplified Technical English, Issue 9, 2025-01-15 (the user's own copy).
- ASD STEMG white paper, ASD-STE100 Simplified Technical English and Artificial Intelligence (the user's own copy).
- Blog test of an STE suffix on model answers: https://www.s-anand.net/blog/simple-writing-hurts-thinking/
