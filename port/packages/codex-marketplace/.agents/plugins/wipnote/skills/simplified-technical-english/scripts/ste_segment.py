"""Split plain text into units and sentences, and count words for sentence length.

This module only segments and counts. The limits and the decisions about them
live in ste_lint.py (one reason to change per module).

Counting follows the word-count rules in Section 8 as far as plain text allows:
text in parentheses is one word and also forms its own sentence (Rule 8.5);
numbers, number plus unit, quoted text and hyphenated words are one word
(Rules 8.6, 8.7); a colon that ends a list introduction ends the sentence
(Rule 8.4). Titles, labels and proper nouns cannot be detected, so a count can
be higher than the true count.
"""

from __future__ import annotations

import bisect
import re
from dataclasses import dataclass, field

MASK = "\x00"

STEP_RE = re.compile(r"^[ \t]*(?:\(?\d+[.)]|\([A-Za-z0-9]+\)|[A-Za-z][.)])[ \t]+")
ITEM_RE = re.compile(r"^[ \t]*[-*•–][ \t]+")
PREFIX_RE = re.compile(r"^[ \t]*(NOTE|WARNING|CAUTION|DANGER)[ \t]*:[ \t]*", re.IGNORECASE)
FENCE_RE = re.compile(r"^[ \t]*(```|~~~)")
QUOTE_RE = re.compile(r"“[^”]*”|\"[^\"\n]*\"")
PAREN_RE = re.compile(r"\([^()]*\)")
NUMBER_RE = re.compile(r"^[+-]?\d[\d.,/]*$")

# Own list of common units: a number followed by one of these is one word.
UNITS = frozenset(
    ["mm", "cm", "m", "km", "in", "ft", "yd", "mi", "g", "kg", "lb", "lbs", "oz", "ml", "l", "psi", "bar", "kpa", "mpa", "pa", "v", "mv", "kv", "a", "ma", "w", "kw", "hz", "khz", "mhz", "ghz", "ohm", "ohms", "nm", "n", "s", "ms", "min", "h", "hr", "deg", "percent", "kb", "mb", "gb", "tb"]
)
# Own list of words whose final period is not a sentence end.
ABBREVS = frozenset(["no", "fig", "figs", "para", "approx", "vs", "cf", "dr", "mr", "mrs", "ms", "st", "a.m", "p.m", "e.g", "i.e"])


@dataclass
class Unit:
    """One run of wrapped text: a step, list item, note, warning or paragraph line group."""

    start: int
    end: int
    kind: str  # "step", "item", "note", "safety" or "prose"
    block: int
    text: str = ""
    parent_kind: str = "prose"  # kind of the last non-item unit before this one


@dataclass
class Sentence:
    start: int
    text: str
    words: int
    unit: Unit = field(repr=False)
    inner: bool = False  # True for text taken from inside parentheses (Rule 8.5)


class LineIndex:
    """Map an absolute character offset to a 1-based line and column."""

    def __init__(self, text: str) -> None:
        self._starts = [0] + [m.end() for m in re.finditer("\n", text)]

    def locate(self, offset: int) -> tuple[int, int]:
        line = bisect.bisect_right(self._starts, offset) - 1
        return line + 1, offset - self._starts[line] + 1


def _classify(line: str) -> tuple[str, int]:
    """Return the unit kind and the width of the marker that opens the line."""
    for kind, regex in (("note", PREFIX_RE), ("step", STEP_RE), ("item", ITEM_RE)):
        match = regex.match(line)
        if match:
            if kind == "note":
                label = match.group(1).upper()
                kind = "note" if label == "NOTE" else "safety"
            return kind, match.end()
    return "", 0


def build_units(text: str) -> list[Unit]:
    """Group the lines of text into units. Code fences and headings are skipped."""
    units: list[Unit] = []
    current: Unit | None = None
    block, in_fence, parent = 0, False, "prose"
    offset = 0
    for raw in text.splitlines(keepends=True):
        line = raw.rstrip("\r\n")
        line_start, offset = offset, offset + len(raw)
        if FENCE_RE.match(line):
            in_fence, current = not in_fence, None
            continue
        if in_fence or line.lstrip().startswith("#"):
            current = None
            continue
        if not line.strip():
            current, block = None, block + (1 if units else 0)
            continue
        kind, width = _classify(line)
        if kind or current is None:
            kind = kind or "prose"
            if kind != "item":
                parent = kind
            current = Unit(line_start + width, line_start + len(line), kind, block,
                           parent_kind=parent)
            units.append(current)
        else:
            current.end = line_start + len(line)
    for unit in units:
        unit.text = text[unit.start:unit.end]
    return units


def mask_spans(text: str, regexes: tuple[re.Pattern[str], ...]) -> str:
    """Replace every span matched by the regexes with MASK characters, keeping offsets."""
    for regex in regexes:
        text = regex.sub(lambda m: MASK * len(m.group()), text)
    return text


def _is_sentence_end(masked: str, pos: int) -> bool:
    """Decide whether the terminator at pos ends a sentence."""
    before = masked[:pos].split()
    word = before[-1].lower().strip("(“\"") if before else ""
    return not (word in ABBREVS or re.fullmatch(r"[a-z]", word) and before[-1].isupper())


def split_sentences(masked: str) -> list[tuple[int, str]]:
    """Return (offset, text) for each sentence in masked text."""
    out: list[tuple[int, str]] = []
    begin = 0
    for match in re.finditer(r"[.!?]+(?=\s+[A-Z\x00]|\s*$)", masked):
        if not _is_sentence_end(masked, match.start()):
            continue
        out.append((begin, masked[begin:match.end()]))
        begin = match.end()
    if masked[begin:].strip():
        out.append((begin, masked[begin:]))
    return [(o, s) for o, s in out if s.strip()]


def count_words(sentence: str) -> int:
    """Count words in a masked sentence by the Section 8 conventions."""
    tokens = [t for t in sentence.split() if re.search(r"[\w\x00]", t)]
    total, i = 0, 0
    while i < len(tokens):
        total += 1
        word = tokens[i].strip(".,:!?").lower()
        nxt = tokens[i + 1].strip(".,:!?").lower() if i + 1 < len(tokens) else ""
        number_follows = i + 1 < len(tokens) and NUMBER_RE.match(nxt)
        if NUMBER_RE.match(word) and nxt in UNITS:
            i += 1  # number plus unit is one word
        elif word == "no" and number_follows:
            i += 1  # "No. 1" is one identifier
        i += 1
    return total


def sentences_of(unit: Unit) -> list[Sentence]:
    """Sentences of a unit, plus one extra sentence for each parenthesised text."""
    masked = mask_spans(unit.text, (QUOTE_RE, PAREN_RE))
    result = [Sentence(unit.start + off, s, count_words(s), unit)
              for off, s in split_sentences(masked)]
    quote_masked = mask_spans(unit.text, (QUOTE_RE,))
    for match in PAREN_RE.finditer(quote_masked):
        inner = match.group()[1:-1]
        inner_masked = mask_spans(inner, (QUOTE_RE,))
        if len(inner.split()) > 1:
            result.append(Sentence(unit.start + match.start() + 1, inner_masked,
                                   count_words(inner_masked), unit, True))
    return result
