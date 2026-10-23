# simplified-technical-english (Claude skill)

Helps an assistant write, rewrite and review text in the style of ASD-STE100 Simplified Technical English,
Issue 9. It is a personal skill, not a git repository. It does not certify compliance, and ASD has not
checked or endorsed it.

## What is here

| Path | Purpose |
|---|---|
| `SKILL.md` | The skill: when to use it and the draft, convert, lint, look up, label workflow. |
| `references/rules-summary.md` | All rules and general recommendations in original words, marked script / dictionary / judgement. |
| `references/evidence-notes.md` | What is verified, the evidence behind the workflow, the AI white paper, the licence. |
| `scripts/ste_lint.py` | Linter for the mechanical rules only. Reads a file or stdin. Exit code 1 on findings. |
| `scripts/ste_segment.py` | Sentence splitting and word counting used by the linter. |
| `scripts/build_dictionary.py` | Builds a local SQLite lookup from your own copy of the spec. |
| `scripts/lookup.py` | Prints approved, unapproved with alternative, or not found, per word. |
| `tests/` | pytest tests. They use a small synthetic fixture, never the real spec. |
| `source/` | Your own download of the spec PDF. See the licence note. |

## Licence note

The ASD-STE100 specification is copyrighted by ASD. It may not be reproduced or published, in whole or in
part, without ASD's written authority. This skill folder contains no ASD text: the rule summaries are written
in original words, and there is no copy of the dictionary or of any word list.

`source/` holds the user's own download of the specification. It is a local copy only. Do not share it, do not
commit it, and do not load it into a prompt. The same applies to the SQLite file that `build_dictionary.py`
creates: it is derived from the spec, so keep it local.

## Build the dictionary from your own spec download

1. Download the specification from ASD and save the PDF as `source/ASD-STE100_ISSUE9.pdf`,
   or keep it anywhere and pass the path.
2. Install `pdftotext` (poppler). On macOS: `brew install poppler`.
3. Run:

   ```
   uv run scripts/build_dictionary.py                       # default path in source/
   uv run scripts/build_dictionary.py /path/to/spec.pdf     # or an explicit path
   STE_SPEC_PDF=/path/to/spec.pdf uv run scripts/build_dictionary.py
   ```

   The path argument wins over `STE_SPEC_PDF`, which wins over the default. The database is written to
   `${XDG_CACHE_HOME:-~/.cache}/simplified-technical-english/ste-dictionary.sqlite3`, never into this folder.
   The script prints the entry count and any entries it could not parse.
4. Look words up: `uv run scripts/lookup.py remove utilize "turn off"`. Without the database, the lookup says
   how to build it and exits with status 2.

## Use the linter

```
uv run scripts/ste_lint.py draft.txt
cat draft.txt | uv run scripts/ste_lint.py --mode procedure
uv run scripts/ste_lint.py --show-limits
```

Findings print as `line:col  rule  message`. Lines that start with `NOTE:`, `WARNING:` or `CAUTION:`, and
numbered or lettered steps, select the matching limit. The linter checks a subset of the rules. A clean
run does not mean the text complies.

## Run the tests

```
uv run --with pytest pytest
```

Run it from this folder. It needs no change to any project dependency list.
