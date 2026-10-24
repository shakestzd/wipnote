#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
"""Deterministic linter for the mechanical subset of ASD-STE100 Issue 9 rules.

Usage: ste_lint.py [--mode auto|procedure|descriptive] [FILE ...]
       ste_lint.py --print-disclosure
       ste_lint.py --show-limits

Reads FILE (or stdin for `-`), prints findings as `line:col  rule  message`,
and exits 0 when clean, 1 for findings, or 2 for input errors. It checks a small
subset of the rules. A clean run does not mean the text complies with the standard.

Line prefixes set the text type: `NOTE:` uses the note limit, `WARNING:` and
`CAUTION:` use the safety limit, numbered or lettered steps use the procedure
limit. Other text uses the descriptive limit unless --mode procedure is given.
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path

from ste_segment import (
    QUOTE_RE,
    LineIndex,
    Sentence,
    Unit,
    build_units,
    mask_spans,
    sentences_of,
)

# --------------------------------------------------------------------------
# Constants. Every limit lives here once, with the rule that sets it.
# --------------------------------------------------------------------------
MAX_WORDS_PROCEDURE = 20  # Rule 5.1; Rule 8.4 applies it before a colon and to each list item
MAX_WORDS_DESCRIPTIVE = 25  # Rule 6.3; Rule 8.4 applies it before a colon and to each list item
MAX_WORDS_NOTE = 25  # Rule 5.5 (a note follows the descriptive limit)
MAX_WORDS_SAFETY = MAX_WORDS_PROCEDURE  # Assumption: Section 7 gives no limit; uses Rule 5.1
MAX_SENTENCES_PER_PARAGRAPH = 6  # Rule 6.6
MAX_HYPHEN_GROUP_WORDS = 3  # Rule 2.2 (and Rule 2.1): no hyphen group of more words

DISCLOSURE = (
    "AI-assisted draft in the style of ASD-STE100. "
    "Not checked or endorsed by ASD. Review by a person is required."
)
FOOTER = (
    ("# Subset check only. This tool tests sentence and paragraph length, the semicolon, "
    "contractions, some verb-form hints, and a few word-level hints."),
    ("# It does not check the dictionary, meanings, noun clusters, articles, or most "
    "other rules. A clean result does not mean the text complies with ASD-STE100."),
    ("# Word counts may be too high where titles, labels, or proper nouns appear "
    "(Rule 8.6). A person must review the text."),
)

# -ed words that are not past participles (need, seed, ...). Excluded from the -ed rule.
_NOT_PARTICIPLE_ED = (r"(?!(?:need|seed|feed|speed|breed|bleed|indeed|proceed|succeed|exceed|"
                      r"heed|weed|greed|deed|creed|steed|reed|bed|red|wed)\b)")
_PARTICIPLE = (rf"(?:{_NOT_PARTICIPLE_ED}\w+ed|taken|given|seen|done|gone|broken|written|shown|"
               r"known|chosen|fallen|worn|torn|driven|risen|made|held|kept|left|lost|set|put|cut|run)")
_IRREGULAR_PP = (r"(?:taken|given|seen|done|gone|broken|written|shown|known|chosen|fallen|"
                 r"worn|torn|driven|risen|made|held|kept|left|lost|begun|become)")
# Whole British forms (no suffix needed).
_BRITISH = (r"colour|centre|metre|litre|grey|behaviour|favour|"
            r"neighbour|catalogue|programme|tyre|aluminium|whilst|labour|honour|fibre|"
            r"sulphur|manoeuvre")
# British -ise stems: match only the -ise verb and -isation/-iser forms, so "organism" and
# the US noun "organization" do not match. Stem + suffix must end the word.
_BRITISH_ISE = (r"(?:organis|utilis|analys|recognis|minimis|maximis|optimis|normalis|"
                r"customis|summaris|prioritis|standardis|categoris|authoris|visualis|"
                r"stabilis|localis|generalis|finalis)(?:e|es|ed|ing|ation|ations|ational|"
                r"er|ers)")
# British doubled-l forms. US "cancellation" and "traveler" are valid, so only the
# inflected British forms match.
_BRITISH_DOUBLE_L = r"(?:travell|cancell)(?:ed|ing|er|ers)"
_ING_NOUNS = {"during", "bearing", "housing", "fitting", "wiring", "spring", "string",
              "ceiling", "lighting", "casting", "bushing", "flooring", "padding", "coating"}


@dataclass(frozen=True)
class Finding:
    line: int
    col: int
    rule: str
    message: str

    def format(self) -> str:
        return f"{self.line}:{self.col}  {self.rule}  {self.message}"


# (rule, message, regex) for checks that scan the text with quotes masked.
PATTERN_CHECKS: tuple[tuple[str, str, re.Pattern[str]], ...] = (
    ("8.1", "Semicolon. Write two sentences.", re.compile(r";")),
    ("4.2", "Contraction. Write the words in full.", re.compile(
        r"\b\w+n't\b|\b(?:it|that|there|here|what|let|he|she)'s\b|\b\w+'(?:re|ve|ll|d|m)\b",
        re.IGNORECASE)),
    ("3.4", "Hint: perfect tense (has/have/had + past participle). Use the simple past.",
     re.compile(rf"\b(?:has|have|had)\s+(?:not\s+)?(?:been|{_PARTICIPLE})\b", re.IGNORECASE)),
    ("3.6", "Passive voice with an agent. Make the agent the subject.", re.compile(
        rf"\b(?:is|are|was|were|be|been|being)\s+(?:\w+ly\s+)?(?:\w+ed|{_IRREGULAR_PP})"
        r"\s+by\b", re.IGNORECASE)),
    ("3.5", "Hint: '-ing' form after a function word. Allowed only in a technical noun.",
     re.compile(r"\b(?:by|before|after|while|when|upon|without|for|of)\s+(\w{4,}ing)\b", re.IGNORECASE)),
    ("GR-6", "Latin abbreviation. Write the English words.", re.compile(
        r"(?<!\w)(?:e\.g\.?|i\.e\.?|etc\.?|et al\.?|viz\.?|cf\.|vs\.?)(?!\w)", re.IGNORECASE)),
    ("GR-3", "Gender-specific pronoun. Use a noun or rewrite (also GR-7).", re.compile(
        r"\b(?:he|she|him|his|her|hers|himself|herself)\b", re.IGNORECASE)),
    ("2.2", "Hyphen group of more words than allowed. Shorten it.", re.compile(
        rf"\b[^\W\d_]+(?:-[^\W\d_]+){{{MAX_HYPHEN_GROUP_WORDS},}}\b")),
    ("1.14", "Hint: British spelling. Use American spelling.", re.compile(
        rf"\b\w*(?:{_BRITISH}|{_BRITISH_ISE}|{_BRITISH_DOUBLE_L})\b", re.IGNORECASE)),
)


def limit_for(unit: Unit, mode: str) -> tuple[int, str]:
    """Return the word limit and the rule that sets it for a unit."""
    if unit.kind == "note":
        return MAX_WORDS_NOTE, "5.5"
    if unit.kind == "safety":
        return MAX_WORDS_SAFETY, "5.1"
    kind = unit.parent_kind if unit.kind == "item" else unit.kind
    if kind == "step" or (mode == "procedure" and kind == "prose"):
        return MAX_WORDS_PROCEDURE, "5.1"
    if kind in ("note", "safety"):
        return limit_for(Unit(0, 0, kind, 0), mode)
    return MAX_WORDS_DESCRIPTIVE, "6.3"


def check_sentence_length(sentence: Sentence, mode: str, where: LineIndex) -> list[Finding]:
    limit, rule = limit_for(sentence.unit, mode)
    if sentence.words <= limit:
        return []
    line, col = where.locate(sentence.start + len(sentence.text) - len(sentence.text.lstrip()))
    return [Finding(line, col, rule,
                    f"Sentence has {sentence.words} words. The limit is {limit}.")]


def check_paragraphs(units: list[Unit], sentences: dict[int, list[Sentence]],
                     where: LineIndex) -> list[Finding]:
    """Rule 6.6: count sentences in each prose-only block."""
    findings: list[Finding] = []
    blocks: dict[int, list[Unit]] = {}
    for unit in units:
        blocks.setdefault(unit.block, []).append(unit)
    for members in blocks.values():
        if any(u.kind != "prose" for u in members):
            continue
        count = sum(1 for u in members for s in sentences[id(u)] if not s.inner)
        if count > MAX_SENTENCES_PER_PARAGRAPH:
            line, col = where.locate(members[0].start)
            findings.append(Finding(line, col, "6.6", f"Paragraph has {count} sentences. "
                                    f"The limit is {MAX_SENTENCES_PER_PARAGRAPH}."))
    return findings


def check_patterns(unit: Unit, where: LineIndex) -> list[Finding]:
    scan = mask_spans(unit.text, (QUOTE_RE,)).replace("’", "'")
    out: list[Finding] = []
    for rule, message, regex in PATTERN_CHECKS:
        for match in regex.finditer(scan):
            if rule == "3.5" and match.group(1).lower() in _ING_NOUNS:
                continue
            line, col = where.locate(unit.start + match.start())
            out.append(Finding(line, col, rule, message))
    return out


def lint(text: str, mode: str = "auto") -> list[Finding]:
    """Return all findings for text, sorted by position."""
    where = LineIndex(text)
    units = build_units(text)
    findings: list[Finding] = []
    by_unit: dict[int, list[Sentence]] = {}
    for unit in units:
        by_unit[id(unit)] = sentences_of(unit)
        for sentence in by_unit[id(unit)]:
            findings += check_sentence_length(sentence, mode, where)
        findings += check_patterns(unit, where)
    findings += check_paragraphs(units, by_unit, where)
    return sorted(set(findings), key=lambda f: (f.line, f.col, f.rule))


def limits_lines() -> list[str]:
    """One line per MAX_ constant, read from this module so no limit is typed twice."""
    return [f"{name} = {value}" for name, value in sorted(globals().items())
            if name.startswith("MAX_")]


def read_source(name: str) -> str:
    """Read a file or stdin as strict UTF-8, without surrogate escapes."""
    if name == "-":
        return sys.stdin.buffer.read().decode("utf-8")
    return Path(name).read_text(encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Lint text for mechanical ASD-STE100 rules.")
    parser.add_argument("files", nargs="*", help="Files to read; '-' means stdin. Default: stdin.")
    parser.add_argument("--mode", choices=("auto", "procedure", "descriptive"), default="auto")
    parser.add_argument("--print-disclosure", action="store_true",
                        help="Print the AI-disclosure footer line and exit.")
    parser.add_argument("--show-limits", action="store_true",
                        help="Print every limit this tool uses, with its name, and exit.")
    args = parser.parse_args(argv)
    if args.show_limits:
        print("\n".join(limits_lines()))
        return 0
    if args.print_disclosure:
        print(DISCLOSURE)
        return 0
    total = 0
    sources = []
    for name in args.files or ["-"]:
        try:
            sources.append((name, read_source(name)))
        except (OSError, UnicodeError) as exc:
            print(f"ste_lint.py: error: cannot read {name!r}: {exc}", file=sys.stderr)
            return 2
    for name, text in sources:
        prefix = f"{name}:" if len(sources) > 1 else ""
        for finding in lint(text, args.mode):
            print(prefix + finding.format())
            total += 1
    print(f"# {total} finding(s).")
    print("\n".join(FOOTER))
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
