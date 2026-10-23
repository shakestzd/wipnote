#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
"""Look up all dictionary senses together; this is not contextual POS tagging."""

from __future__ import annotations

import argparse
import re
import sqlite3
import sys
from collections import Counter
from contextlib import closing
from pathlib import Path

from build_dictionary import POS, db_path

CONTRACT = """Output contract:
  One line per distinct lowercase term, in first-seen order:
    term[:pos]: STATUS; (pos) approved; (pos) unapproved -> ALTERNATIVES
  Only matching senses are printed; listed forms add ' [form of HEADWORD]'.
  STATUS is approved (approved senses only), unapproved (unapproved senses
  only), check-pos (both approved and unapproved senses), or not-found.
  Alternatives retain their POS or TN/TV tags; alternatives use ' | ' as a
  separator. Without an alternative, print 'help: NOTE' or 'no alternative given'.
  A POS hint (word:n, word:v, word:adj, word:adv, or another dictionary POS)
  restricts both headword and listed-form matches to that POS.
  Arguments are terms (quote phrases). Stdin is plain text; --lines instead
  reads one term per line. Strip surrounding punctuation and deduplicate terms.
  On stdin, skip numbers, tokens containing digits or underscores (part-number
  candidates), and all-uppercase tokens with two or more letters (identifiers).
  Explicit POS hints bypass identifier skipping; arguments are never skipped.
  Print 'skipped: N' (token occurrences) before the one-line summary footer:
    summary: approved=N unapproved=N check-pos=N not-found=N
  Exit 0: no unapproved/check-pos terms; exit 1: either status is present;
  exit 2: database missing. Not-found is not approval. No contextual POS is inferred.
"""
MISSING_MESSAGE = (
    "error: the dictionary database does not exist: {path}\n"
    "Build it from your own copy of the ASD-STE100 PDF:\n"
    "  uv run scripts/build_dictionary.py /path/to/ASD-STE100_ISSUE9.pdf\n"
    "No result is better than a guess, so this lookup stops here."
)
STATUSES = ("approved", "unapproved", "check-pos", "not-found")
TOKEN_RE = re.compile(rf"\w+(?:[-'’./]\w+)*(?::(?:{POS}))?", re.IGNORECASE)


def clean(term: str) -> str:
    """Strip surrounding punctuation, preserving internal phrase punctuation."""
    return term.strip().strip(".,;:!?()[]{}<>\"'“”‘’—–").lower()


def split_hint(term: str) -> tuple[str, str | None]:
    word, sep, hint = term.rpartition(":")
    return (word, hint) if sep and hint in POS.split("|") else (term, None)


def matching_rows(con: sqlite3.Connection, term: str) -> list[tuple]:
    """Union direct and listed-form senses; forms must not hide a direct sense."""
    word, hint = split_hint(term)
    rows = con.execute(
        "SELECT word, pos, approved, meaning, alternatives FROM entries WHERE word = ? "
        "UNION SELECT e.word, e.pos, e.approved, e.meaning, e.alternatives "
        "FROM forms f JOIN entries e ON e.word=f.word AND e.pos=f.pos "
        "AND e.approved=f.approved WHERE f.form = ? "
        "ORDER BY approved DESC, pos, word", (word, word)).fetchall()
    return [row for row in rows if hint is None or row[1] == hint]


def describe(row: tuple, term: str) -> str:
    word, pos, approved, meaning, alternatives = row
    detail = f"({pos}) approved" if approved else f"({pos}) unapproved -> " + (
        alternatives or (f"help: {meaning}" if meaning else "no alternative given"))
    if word != split_hint(term)[0]:
        detail += f" [form of {word}]"
    return detail


def lookup_result(con: sqlite3.Connection, term: str) -> tuple[str, str]:
    rows = matching_rows(con, term)
    approvals = {bool(row[2]) for row in rows}
    status = ("check-pos" if len(approvals) == 2 else
              "approved" if True in approvals else
              "unapproved" if rows else "not-found")
    details = "; ".join(describe(row, term) for row in rows)
    return status, f"{term}: {status}" + (f"; {details}" if details else "")


def lookup_terms(con: sqlite3.Connection, terms: list[str]) -> list[str]:
    """One grouped line per distinct normalized term (without CLI footer)."""
    terms = list(dict.fromkeys(term for raw in terms if (term := clean(raw))))
    return [lookup_result(con, term)[1] for term in terms]


def skip_token(raw: str) -> bool:
    if split_hint(clean(raw))[1] is not None:
        return False
    letters = "".join(c for c in raw if c.isalpha())
    return (any(c.isdigit() for c in raw) or "_" in raw
            or (len(letters) >= 2 and letters.isupper()))


def read_terms(args: list[str], stdin_lines: bool) -> tuple[list[str], int]:
    if args:
        return args, 0
    text = sys.stdin.read()
    tokens = text.splitlines() if stdin_lines else TOKEN_RE.findall(text)
    skipped = sum(skip_token(raw) for raw in tokens)
    return [raw for raw in tokens if not skip_token(raw)], skipped


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, epilog=CONTRACT,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--lines", action="store_true", help="read stdin terms one per line")
    parser.add_argument("terms", nargs="*", metavar="WORD[:POS]")
    args = parser.parse_args(argv)
    path: Path = db_path()
    if not path.is_file():
        print(MISSING_MESSAGE.format(path=path), file=sys.stderr)
        return 2
    terms, skipped = read_terms(args.terms, args.lines)
    terms = list(dict.fromkeys(term for raw in terms if (term := clean(raw))))
    counts: Counter[str] = Counter()
    with closing(sqlite3.connect(f"{path.as_uri()}?mode=ro", uri=True)) as con:
        for term in terms:
            status, line = lookup_result(con, term)
            counts[status] += 1
            print(line)
    print(f"skipped: {skipped}")
    print("summary: " + " ".join(f"{status}={counts[status]}" for status in STATUSES))
    return int(bool(counts["unapproved"] or counts["check-pos"]))


if __name__ == "__main__":
    sys.exit(main())
