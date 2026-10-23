#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
"""Build a local SQLite lookup from your own copy of the ASD-STE100 specification.

Usage: build_dictionary.py [PDF]

PDF defaults to the env var STE_SPEC_PDF, then to source/ASD-STE100_ISSUE9.pdf
beside this script's parent folder. The database goes to
${XDG_CACHE_HOME:-~/.cache}/simplified-technical-english/ste-dictionary.sqlite3
and never inside the skill folder. The dictionary is ASD's copyrighted work:
keep the database local and never commit or share it.

The parser reads the `pdftotext -layout` text of Part 2. Column 1 holds the word
and part of speech: UPPERCASE means approved, lowercase means not approved.
Column 2 holds the approved meaning, or the approved alternatives.
"""

from __future__ import annotations

import os
import re
import shutil
import sqlite3
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

PDFTOTEXT = (os.environ.get("PDFTOTEXT") or shutil.which("pdftotext")
             or "/opt/homebrew/bin/pdftotext")
DB_NAME = "ste-dictionary.sqlite3"
DEFAULT_PDF = Path(__file__).resolve().parent.parent / "source" / "ASD-STE100_ISSUE9.pdf"

POS = r"n|v|adj|adv|pron|art|prep|conj|prefix|phrase"
ENTRY_RE = re.compile(rf"^(?P<word>[A-Za-z][^()]*?(?:\((?!(?:{POS})\))[^)]*\)[^()]*)?)"
                      rf"\s*\((?P<pos>{POS})\)")
TAIL_RE = re.compile(rf"^(?P<rest>(?:\([^)]*\)\s*)*?)\((?P<pos>{POS})\)")
ALT_RE = re.compile(rf"^([A-Z][A-Z0-9’'/.… −\- ]*?)\s*\(({POS}|TN|TV)\)")
INTRO_END_RE = re.compile(r"Page 2-0-\d+")


def db_path() -> Path:
    base = os.environ.get("XDG_CACHE_HOME") or str(Path.home() / ".cache")
    return Path(base) / "simplified-technical-english" / DB_NAME


@dataclass
class Entry:
    word: str
    pos: str
    approved: bool
    meaning: str = ""
    alternatives: list[str] = field(default_factory=list)
    forms: list[str] = field(default_factory=list)  # other verb forms, lowercase


def _columns(header: str, following: str) -> tuple[int, int]:
    """Return the start of column 2 and column 3 from the two header lines."""
    col2 = header.find("Approved")
    col3 = max(header.find("STE EXAMPLE"), following.find("STE EXAMPLE"))
    return col2, (col3 if col3 > col2 else col2 + 30)


def _dictionary_lines(text: str) -> list[str]:
    """Return the lines of the dictionary body (after the introduction pages)."""
    lines = text.splitlines()
    last_intro = max((i for i, ln in enumerate(lines) if INTRO_END_RE.search(ln)),
                     default=-1)
    return lines[last_intro + 1:]


def _is_upper(word: str) -> bool:
    letters = [c for c in word if c.isalpha()]
    return bool(letters) and all(c.isupper() for c in letters)


def _is_noise(line: str) -> bool:
    s = line.strip()
    return s.startswith(("Issue 9", "Page 2-", "2025-01-15", "ASD-STE100", "ASD STE100",
                         "(part of speech)", "Word ")) or s == "Word"


def _col1_end(line: str, col2: int) -> int:
    """Find row-specific boundaries; PDF rows can drift from the page header."""
    if not line[:1].strip():
        return 0
    gap = re.search(r" {2,}", line)
    field = line[:gap.start()] if gap else line
    match = ENTRY_RE.match(field) or TAIL_RE.match(field)
    if match:
        return match.end() + int(line[match.end():].startswith(","))
    end = gap.start() if gap else len(line)
    # A long uppercase headword can touch its mixed-case meaning with one space.
    mixed = re.search(r" (?=[A-Z][a-z])", line[:end])
    if mixed and _is_upper(line[:mixed.start()]):
        end = mixed.start()
    return end


def _chunk(line: str, col2: int, col3: int) -> str:
    """Keep column 2 only, using whitespace gutters as well as header positions."""
    start = _col1_end(line, col2) if line[:1].strip() else 0
    rest = line[start:]
    indent = len(rest) - len(rest.lstrip())
    absolute = start + indent
    if absolute >= col3 - 3:
        return ""
    body = rest[indent:]
    body = re.split(r" {2,}", body, maxsplit=1)[0]
    body = body[:max(0, col3 - absolute)]
    return " " * max(0, absolute - col2) + body


def _leading_upper(text: str) -> str:
    """Leading uppercase alternative; punctuation ends an untagged phrase."""
    match = re.match(r"[A-Z][A-Z0-9’'… /−\-]*(?=$|[^A-Za-z])", text)
    return match.group().strip() if match else ""


def _fill(entry: Entry, chunks: list[str]) -> Entry:
    """Retain alternative POS/TN/TV tags; never treat a bare tag as help."""
    meaning, alts = [], []
    after_marker = False
    first_indent = None
    qualifier = 0
    for raw in chunks:
        text = raw.strip()
        if not text:
            continue
        if qualifier or (text.startswith("(") and not re.fullmatch(
                rf"\((?:{POS}|TN|TV)\)", text)):
            qualifier += text.count("(") - text.count(")")
            continue
        if "for other" in text.lower() or "meanings, use:" in text.lower():
            after_marker = True
            continue
        indent = len(raw) - len(raw.lstrip())
        if first_indent is None:
            first_indent = indent
        if (not entry.approved and indent == first_indent) or (after_marker and not indent):
            tagged = ALT_RE.match(text)
            alt = f"{tagged[1].strip()} ({tagged[2]})" if tagged else _leading_upper(text)
            if alt:
                alts.append(alt)
                continue
            if re.fullmatch(rf"\((?:{POS}|TN|TV)\)", text) and alts:
                alts[-1] += " " + text
                continue
        if not after_marker:
            meaning.append(text)
    entry.meaning = " ".join(meaning)
    entry.alternatives = list(dict.fromkeys(alts))
    return entry


def _entry_head(col1: str, pending: tuple[str, int] | None) -> tuple[str, str, int | None] | None:
    """Return (word, pos, index of the line where the entry started) or None."""
    if pending and pending[0].count("(") > pending[0].count(")"):
        combined = ENTRY_RE.match(pending[0] + " " + col1)
        if combined:
            return combined["word"].strip(), combined["pos"], pending[1]
    full = ENTRY_RE.match(col1)
    if full and not col1.startswith("("):
        word = full.group("word").strip()
        if pending and pending[0].endswith("-"):
            word = pending[0] + word  # normalize discretionary hyphens after parsing
            return word, full.group("pos"), pending[1]
        if (pending and _is_upper(pending[0]) and _is_upper(word)
                and full["pos"] == "prep"):
            return pending[0] + " " + word, full["pos"], pending[1]
        return word, full.group("pos"), None
    tail = TAIL_RE.match(col1)
    if tail and pending:
        return f"{pending[0]} {tail.group('rest')}".strip(), tail.group("pos"), pending[1]
    return None


class _Parser:
    """Walk the layout lines of the dictionary and collect entries."""

    def __init__(self, usage: set[str]) -> None:
        self.usage = usage
        self.entries: list[Entry] = []
        self.col2 = self.col3 = 0
        self.current: Entry | None = None
        self.chunks: list[str] = []
        self.pending: tuple[str, int] | None = None  # untagged col 1 text, chunk index
        self.in_forms = False  # the last col 1 line was a verb-forms line ending in a comma

    def close(self, upto: int | None = None) -> None:
        if self.current:
            self.entries.append(_fill(self.current, self.chunks[:upto]))

    def flush_pending(self) -> None:
        """Keep a row without a POS tag when the next head proves it is complete."""
        if not self.pending:
            return
        word, index = self.pending
        if index < len(self.chunks) and self.chunks[index].strip():
            self.close(index)
            self.current = Entry(word, "phrase", _is_upper(word))
            self.chunks = self.chunks[index:]
        self.pending = None

    def start(self, head: tuple[str, str, int | None], head_line: str) -> None:
        word, pos, start = head
        word = re.sub(r"\s+\)", ")", re.sub(r"\(\s+", "(", word))
        if self.pending and self.pending[0].endswith("-") and start is not None:
            if word.lower() not in self.usage:
                word = self.pending[0][:-1] + word[len(self.pending[0]):]
        if start is None:
            self.flush_pending()
        self.close(start)
        self.chunks = self.chunks[start:] if start is not None else []
        self.current = Entry(word, pos, _is_upper(re.sub(r"\([^)]*\)", "", word)))
        self.pending = None
        self.in_forms = head_line.endswith(",")
        self.add_forms(head_line)

    def add_forms(self, col1: str) -> None:
        if self.current and self.current.approved:
            body = ENTRY_RE.sub("", col1, count=1)
            self.current.forms += [w.lower() for w in re.findall(r"[A-Z][A-Z'\-]+", body)
                                   if w != "ALSO"]

    def line(self, line: str) -> None:
        col1 = line[:_col1_end(line, self.col2)].strip() if line[:1].strip() else ""
        if self.current and col1.strip(",()").lower() in self.current.forms:
            self.pending = None
            self.in_forms = col1.endswith(",")
            self.chunks.append(_chunk(line, self.col2, self.col3))
            return
        head = _entry_head(col1, self.pending) if col1 else None
        if head:
            self.start(head, col1)
        elif col1 and self.in_forms:
            self.add_forms(col1)
            self.in_forms = col1.endswith(",")
            self.pending = None
        elif col1 and ("," in col1 or col1.startswith("(")):
            self.add_forms(col1)
            self.in_forms = col1.endswith(",")
            self.pending = None
        elif (col1 and self.current and self.current.approved
              and self.current.pos in {"v", "adj"} and _is_upper(col1)
              and (not _chunk(line, self.col2, self.col3).strip()
                   or _chunk(line, self.col2, self.col3).lstrip()[:1].islower())):
            self.add_forms(col1)
            self.pending = None
        elif col1:
            if self.pending and "(" in self.pending[0]:
                col1 = self.pending[0] + " " + col1
            else:
                self.flush_pending()
            self.pending = (col1, len(self.chunks))
        self.chunks.append(_chunk(line, self.col2, self.col3))


def parse_dictionary(text: str) -> list[Entry]:
    """Parse layout text. Keep a wrapped lexical hyphen only if an independent,
    contiguous spelling occurs elsewhere in the source (headword or usage).
    Lowercase continuation alone is not proof. Unattested compounds remain
    ambiguous and lose the layout hyphen; no external vocabulary is guessed.
    """
    lines = _dictionary_lines(text)
    usage = set(re.findall(r"(?i)\b[a-z]+(?:-[a-z]+)+\b", text.lower()))
    parser = _Parser(usage)
    for i, line in enumerate(lines):
        if "Approved meaning" in line:
            parser.col2, parser.col3 = _columns(line, lines[i + 1] if i + 1 < len(lines) else "")
        elif parser.col2 and line.strip() and not _is_noise(line):
            parser.line(line)
    parser.flush_pending()
    parser.close()
    return _dedupe(parser.entries)


def _dedupe(entries: list[Entry]) -> list[Entry]:
    """Merge repeats (an entry that continues over a page break) by word, pos, status."""
    merged: dict[tuple[str, str, bool], Entry] = {}
    for e in entries:
        key = (e.word.lower(), e.pos, e.approved)
        if key in merged:
            old = merged[key]
            old.alternatives += [a for a in e.alternatives if a not in old.alternatives]
            old.forms += [f for f in e.forms if f not in old.forms]
            if e.meaning and e.meaning not in old.meaning:
                old.meaning = f"{old.meaning} {e.meaning}".strip()
        else:
            merged[key] = e
    return list(merged.values())


def write_db(entries: list[Entry], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    tmp.unlink(missing_ok=True)
    con = sqlite3.connect(tmp)
    con.execute("CREATE TABLE entries (word TEXT NOT NULL, pos TEXT NOT NULL, "
                "approved INTEGER NOT NULL, meaning TEXT, alternatives TEXT)")
    con.executemany("INSERT INTO entries VALUES (?, ?, ?, ?, ?)", [
        (e.word.lower(), e.pos, int(e.approved), e.meaning, " | ".join(e.alternatives))
        for e in entries])
    con.execute("CREATE INDEX idx_word ON entries (word)")
    con.execute("CREATE TABLE forms (form TEXT NOT NULL, word TEXT NOT NULL, pos TEXT NOT NULL, "
                "approved INTEGER NOT NULL)")
    con.executemany("INSERT INTO forms VALUES (?, ?, ?, ?)", sorted({
        (f, e.word.lower(), e.pos, int(e.approved)) for e in entries for f in e.forms
        if f != e.word.lower()}))
    con.execute("CREATE INDEX idx_form ON forms (form)")
    con.commit()
    con.close()
    tmp.replace(path)


def _page_columns(lines: list[str], index: int) -> tuple[int, int, int]:
    """Locate physical columns in fixed-pitch output, allowing small row shifts."""
    header, following = lines[index:index + 2]
    col1, col2 = header.index("Word"), header.index("Approved")
    example = re.search(r"STE\s+EXAMPLE", following) or re.search(r"STE\s+EXAMPLE", header)
    if example is None:
        raise ValueError("dictionary header has no example column")
    col3 = example.start()
    starts = [len(line) - len(line.lstrip()) for line in lines if line.strip()]
    col2 = min([col2] + [x for x in starts if col2 - 4 <= x <= col2])
    col3 = min([col3] + [x for x in starts if col3 - 4 <= x <= col3])
    return col1, col2, col3


def _tabular_text(text: str) -> str:
    """Canonicalize fixed-pitch PDF columns before collapsing inter-word spaces.

    Default pdftotext layout changes character pitch within a row, so header
    offsets cannot reliably separate examples. Fixed 3-point pitch preserves
    physical x positions. Canonical cells keep the parser/fixtures independent
    of that pitch. Indented column-1 notes are not dictionary headwords.
    """
    result = []
    for page in text.split("\f"):
        lines = page.splitlines()
        index = next((i for i, line in enumerate(lines)
                      if re.match(r"^\s*Word\s+Approved\s+meaning", line)), None)
        if index is None:
            result.extend(" ".join(line.split()) for line in lines)
            continue
        col1, col2, col3 = _page_columns(lines, index)
        for i, line in enumerate(lines):
            if i == index:
                result.append(f'{"Word":<80}Approved meaning/')
            elif i == index + 1:
                result.append(f'{"(part of speech)":<80}{"ALTERNATIVES":<100}STE EXAMPLE')
            elif i < index or _is_noise(" ".join(line.split())):
                result.append(" ".join(line.split()))
            else:
                cells = [" ".join(cell.split()) for cell in
                         (line[col1:col2], line[col2:col3], line[col3:])]
                if line[:col2].strip() and len(line) - len(line.lstrip()) > col1 + 1:
                    cells[0] = ""
                result.append(f"{cells[0]:<80}{cells[1]:<100}{cells[2]}")
    return "\n".join(result)


def extract_text(pdf: Path) -> str:
    help_text = subprocess.run([PDFTOTEXT, "-h"], capture_output=True, text=True)
    flags = ["-remove-hyphens", "none"] if "-remove-hyphens" in (
        help_text.stdout + help_text.stderr) else []
    result = subprocess.run([PDFTOTEXT, "-layout", "-fixed", "3", *flags, str(pdf), "-"],
                            capture_output=True, text=True, check=True)
    return _tabular_text(result.stdout)


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    pdf = Path(args[0] if args else os.environ.get("STE_SPEC_PDF") or DEFAULT_PDF)
    if not pdf.is_file():
        print(f"error: spec PDF not found: {pdf}\nPass the path of your own copy as an "
              "argument or set STE_SPEC_PDF.", file=sys.stderr)
        return 2
    try:
        entries = parse_dictionary(extract_text(pdf))
    except (FileNotFoundError, subprocess.CalledProcessError) as exc:
        print(f"error: pdftotext failed ({exc}). Install poppler or set PDFTOTEXT.",
              file=sys.stderr)
        return 2
    path = db_path()
    write_db(entries, path)
    approved = sum(e.approved for e in entries)
    print(f"entries: {len(entries)} (approved {approved}, not approved "
          f"{len(entries) - approved})")
    print(f"database: {path}")
    help_only = [e for e in entries if not e.approved and not e.alternatives and e.meaning]
    odd = [e for e in entries if not e.approved and not e.alternatives and not e.meaning]
    odd += [e for e in entries if e.approved and not e.meaning]
    print(f"not-approved entries with a help note and no alternative: {len(help_only)}")
    print(f"entries with no alternative or help (not approved) or no meaning (approved): "
          f"{len(odd)}")
    for e in odd[:20]:
        print(f"  unparsed detail: {e.word} ({e.pos})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
