"""Dictionary build and lookup tests. They use a small synthetic fixture, never the real spec."""

import sqlite3
from pathlib import Path

import build_dictionary as bd
import lookup
import pytest

FIXTURE = Path(__file__).parent / "fixture_layout.txt"


@pytest.fixture(scope="module")
def entries():
    return bd.parse_dictionary(FIXTURE.read_text(encoding="utf-8"))


def find(entries, word, pos=None):
    return [e for e in entries if e.word.lower() == word and (pos is None or e.pos == pos)]


def test_introduction_rows_are_skipped(entries):
    assert find(entries, "zed") == []


def test_uppercase_is_approved_lowercase_is_not(entries):
    assert find(entries, "florp")[0].approved is True
    assert find(entries, "glorp")[0].approved is False


def test_unapproved_entry_keeps_all_alternatives(entries):
    assert find(entries, "glorp")[0].alternatives == ["FLORP (n)", "SNARF (v)"]


def test_untagged_alternative_is_kept(entries):
    assert find(entries, "wibble")[0].alternatives == ["NOT MORE THAN"]


def test_help_only_entry_has_meaning_and_no_alternative(entries):
    quux = find(entries, "quux")[0]
    assert quux.alternatives == []
    assert "different construction" in quux.meaning


def test_approved_meaning_is_joined_across_lines(entries):
    assert find(entries, "florp")[0].meaning == "A thing that is made up for this fixture"


def test_repeated_entry_across_page_break_is_merged(entries):
    assert len(find(entries, "florp", "n")) == 1


def test_verb_forms_are_collected_including_the_last_form(entries):
    assert set(find(entries, "snarf")[0].forms) >= {"snarfs", "snarfed"}


def test_word_wrapped_at_a_hyphen_is_rejoined(entries):
    assert find(entries, "splittable")[0].pos == "adj"


def test_part_of_speech_on_the_next_line(entries):
    assert find(entries, "longwordish")[0].pos == "adj"


def test_second_meaning_is_not_lost(entries):
    assert "To warn" in find(entries, "beep")[0].meaning


def test_phrase_with_qualifier_keeps_pos_from_next_line(entries):
    assert find(entries, "blim (in blim of)")[0].pos == "conj"


def test_no_entry_is_left_without_content(entries):
    for e in entries:
        assert e.alternatives or e.meaning, e.word


@pytest.fixture()
def db(tmp_path, entries, monkeypatch):
    monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path))
    path = bd.db_path()
    bd.write_db(entries, path)
    return path


def run_lookup(db, *terms):
    con = sqlite3.connect(db)
    try:
        return lookup.lookup_terms(con, list(terms))
    finally:
        con.close()


def test_database_lives_under_the_cache_dir(db, tmp_path):
    assert tmp_path in db.parents
    assert "simplified-technical-english" in str(db)


def test_lookup_approved(db):
    assert run_lookup(db, "florp:n") == ["florp:n: approved; (n) approved"]


def test_lookup_unapproved_names_alternatives(db):
    assert run_lookup(db, "glorp") == ["glorp: unapproved; (v) unapproved -> FLORP (n) | SNARF (v)"]


def test_lookup_not_found(db):
    assert run_lookup(db, "nonexistentword") == ["nonexistentword: not-found"]


def test_lookup_verb_form_points_at_its_entry(db):
    assert run_lookup(db, "snarfed:v") == ["snarfed:v: approved; (v) approved [form of snarf]"]


def test_lookup_is_case_and_punctuation_insensitive(db):
    assert run_lookup(db, "Snarf.") == ["snarf: approved; (v) approved"]
    assert run_lookup(db, "(snarf)") == ["snarf: approved; (v) approved"]


@pytest.mark.parametrize("term,line", [
    ("mott", "mott: approved; (adj) approved [entry: mott (or mottie)]"),
    ("mottie", "mottie: approved; (adj) approved [entry: mott (or mottie)]"),
    ("MOTT (or MOTTIE)", "mott (or mottie): approved; (adj) approved"),
    ("blim:conj", "blim:conj: unapproved; (conj) unapproved -> FLORP (n) [entry: blim (in blim of)]"),
    ("blim (in blim of)", "blim (in blim of): unapproved; (conj) unapproved -> FLORP (n)"),
    ("in blim of", "in blim of: unapproved; (conj) unapproved -> FLORP (n) [entry: blim (in blim of)]"),
    ("florp (in a florp)", "florp (in a florp): unapproved; (prep) unapproved -> FLORP (n)"),
])
def test_qualified_headings_are_queryable(db, term, line):
    assert run_lookup(db, term) == [line]


def test_aliases_keep_base_or_spelling_and_usage_phrase():
    assert bd.aliases_of("MOTT (or MOTTIE)") == {"mott", "mottie"}
    assert bd.aliases_of("blim (in blim of)") == {"blim", "in blim of"}
    assert bd.aliases_of("FREE FLOOP") == set()


def test_lookup_reads_a_database_built_before_aliases(tmp_path, entries):
    path = tmp_path / "old.sqlite3"
    bd.write_db(entries, path)
    with sqlite3.connect(path) as con:
        con.execute("DROP TABLE aliases")
    assert run_lookup(path, "snarf") == ["snarf: approved; (v) approved"]
    assert run_lookup(path, "mott") == ["mott: not-found"]


def test_lookup_without_database_exits_nonzero_and_says_how_to_build(tmp_path, monkeypatch,
                                                                     capsys):
    monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path / "empty"))
    assert lookup.main(["florp"]) == 2
    err = capsys.readouterr().err
    assert "build_dictionary.py" in err


def test_empty_parse_keeps_the_existing_database(db, tmp_path, monkeypatch, capsys):
    with sqlite3.connect(db) as con:
        before = con.execute("SELECT COUNT(*) FROM entries").fetchone()[0]
    assert before > 0
    pdf = tmp_path / "wrong.pdf"
    pdf.write_bytes(b"%PDF-1.4 not the spec")
    monkeypatch.setattr(bd, "extract_text", lambda _pdf: "A page with no dictionary header.\n")
    assert bd.main([str(pdf)]) == 2
    assert "left unchanged" in capsys.readouterr().err
    with sqlite3.connect(db) as con:
        assert con.execute("SELECT COUNT(*) FROM entries").fetchone()[0] == before


def test_build_refuses_a_missing_pdf(tmp_path, capsys):
    assert bd.main([str(tmp_path / "missing.pdf")]) != 0
    assert "not found" in capsys.readouterr().err


def test_default_pdf_is_resolved_beside_the_script():
    assert bd.DEFAULT_PDF == Path(bd.__file__).resolve().parent.parent / "source" / bd.DEFAULT_PDF.name


def test_mixed_senses_are_check_pos_not_unapproved(db):
    lines = run_lookup(db, "Florp.", "florp")
    assert lines == ["florp: check-pos; (n) approved; "
                     "(prep) unapproved -> FLORP (n) [entry: florp (in a florp)]; "
                     "(v) unapproved -> FLORP (n)"]
    assert ": unapproved;" not in lines[0]


def test_pos_hint_excludes_the_other_sense(db):
    assert run_lookup(db, "florp:n") == ["florp:n: approved; (n) approved"]
    assert run_lookup(db, "florp:v") == [
        "florp:v: unapproved; (v) unapproved -> FLORP (n)"]
    assert run_lookup(db, "florp:adj") == ["florp:adj: not-found"]
    assert run_lookup(db, "florp:adv") == ["florp:adv: not-found"]


def test_direct_and_inflected_senses_are_grouped(db):
    lines = run_lookup(db, "snarfed")
    assert lines == ["snarfed: check-pos; (v) approved [form of snarf]; "
                     "(n) unapproved -> FLORP (n)"]


def test_uppercase_spill_is_not_an_alternative(entries):
    assert find(entries, "spillage")[0].alternatives == ["FLORP (n)"]
    assert all("SPURIOUS" not in alt for e in entries for alt in e.alternatives)
    assert find(entries, "wibble")[0].alternatives == ["NOT MORE THAN"]


def test_hyphen_requires_independent_source_evidence(entries):
    assert find(entries, "zig-zag")[0].pos == "v"
    assert not find(entries, "zigzag")
    assert find(entries, "rustical")[0].pos == "adj"
    assert not find(entries, "rusti-cal")
    assert find(entries, "zo-")[0].pos == "prefix"


def test_row_specific_boundaries_and_qualifiers(entries):
    assert find(entries, "zigglilily")[0].approved
    assert not find(entries, "zigglilily a")
    assert find(entries, "mott (or mottie)")[0].approved
    assert find(entries, "florp (in a florp)")[0].alternatives == ["FLORP (n)"]
    assert len(find(entries, "downfloop of")) == 1
    assert not find(entries, "of")


def test_untagged_phrases_and_forms_without_commas(entries):
    assert find(entries, "free floop")[0].approved
    assert not find(entries, "suchfloop")[0].approved
    assert find(entries, "suchfloop")[0].alternatives == ["FREE FLOOP"]
    assert set(find(entries, "plink")[0].forms) == {"plinks", "plunked"}
    assert not find(entries, "plunked")


def test_stdin_deduplication_skips_and_footer(db, monkeypatch, capsys):
    import io

    monkeypatch.setattr("sys.stdin", io.StringIO(
        "Snarf, snarf. SNARF 123 36L7 AB-12 R_foo florp:n; nonexistentword!"))
    assert lookup.main([]) == 0
    assert capsys.readouterr().out.splitlines() == [
        "snarf: approved; (v) approved", "florp:n: approved; (n) approved",
        "nonexistentword: not-found", "skipped: 5",
        "summary: approved=2 unapproved=0 check-pos=0 not-found=1"]


@pytest.mark.parametrize("terms,status", [
    (["snarf"], 0), (["nonexistentword"], 0), (["glorp"], 1),
    (["florp"], 1), (["florp:n"], 0), (["florp:v"], 1),
])
def test_exit_codes_and_summary(db, capsys, terms, status):
    assert lookup.main(terms) == status
    lines = capsys.readouterr().out.splitlines()
    assert lines[-2] == "skipped: 0"
    assert lines[-1].startswith("summary: approved=")
    assert "check-pos=1" in lines[-1] if terms == ["florp"] else "check-pos=0" in lines[-1]


def test_lines_and_quoted_phrase_lookup(db, monkeypatch, capsys):
    import io

    monkeypatch.setattr("sys.stdin", io.StringIO("Free floop\nfree floop\n"))
    assert lookup.main(["--lines"]) == 0
    assert capsys.readouterr().out.splitlines() == [
        "free floop: approved; (phrase) approved", "skipped: 0",
        "summary: approved=1 unapproved=0 check-pos=0 not-found=0"]
    assert run_lookup(db, "FREE FLOOP") == ["free floop: approved; (phrase) approved"]


def test_help_is_available_without_database(tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path / "absent"))
    with pytest.raises(SystemExit) as exc:
        lookup.main(["--help"])
    assert exc.value.code == 0
    assert lookup.CONTRACT in capsys.readouterr().out


def test_multiple_approved_senses_do_not_make_a_finding(tmp_path):
    path = tmp_path / "synthetic.sqlite3"
    bd.write_db([bd.Entry("zorp", "n", True), bd.Entry("zorp", "v", True)], path)
    assert run_lookup(path, "zorp") == ["zorp: approved; (n) approved; (v) approved"]


def test_parser_header_between_head_and_pos():
    text = FIXTURE.read_text(encoding="utf-8")
    text = text.replace(
        "\n(adj)\nBEEP", "\nIssue 9\nPage 2-1-X1\n"
        "Word               Approved meaning/\n"
        "(part of speech)   ALTERNATIVES              STE EXAMPLE\n(adj)\nBEEP")
    rows = bd.parse_dictionary(text)
    assert find(rows, "longwordish")[0].pos == "adj"
    assert len(find(rows, "longwordish")) == 1


def test_scripts_stay_small():
    import ast

    for module in (bd, lookup):
        text = Path(module.__file__).read_text(encoding="utf-8")
        assert len(text.splitlines()) < 500
        for node in ast.walk(ast.parse(text)):
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                assert node.end_lineno - node.lineno + 1 < 50, node.name


def test_fixed_pitch_column_separation_before_whitespace_collapse():
    def row(left, middle, right):
        return f"    {left:<40}{middle:<50}{right}"

    text = "\n".join([
        row("Word", "Approved meaning/", ""),
        row("(part of speech)", "ALTERNATIVES", "STE EXAMPLE"),
        row("snazzle (v)", "FREE   FLOOP", "STRAY  EXAMPLE WORDS"),
        row("FREE   FLOOP", "A  synthetic phrase", "FREE FLOOP."),
        row("flib (v)", "FLORP (n)", "EXAMPLE."),
        row("", "(WITH A SYNTHETIC [TN]", "EXAMPLE."),
        row("", "OR SYNTHETICS [TN])", "EXAMPLE."),
        row("", "SNARF (v)", "EXAMPLE."),
        row("FLORP (n)", "A  synthetic thing", "AN EXAMPLE."),
    ])
    entries = bd.parse_dictionary(bd._tabular_text(text))
    assert find(entries, "snazzle")[0].alternatives == ["FREE FLOOP"]
    assert find(entries, "free floop")[0].approved
    assert find(entries, "flib")[0].alternatives == ["FLORP (n)", "SNARF (v)"]
    assert not find(entries, "or synthetics")
    assert find(entries, "florp")[0].meaning == "A synthetic thing"


def test_tag_only_continuation_does_not_become_empty_help():
    text = "\n".join([
        "Word               Approved meaning/",
        "(part of speech)   ALTERNATIVES              STE EXAMPLE",
        "snazzle (v)        FLORP                     EXAMPLE.",
        "                   (n)",
    ])
    entry = bd.parse_dictionary(text)[0]
    assert entry.alternatives == ["FLORP (n)"]
    assert entry.meaning == ""


def test_extract_uses_fixed_layout_and_preserves_hyphens(monkeypatch, tmp_path):
    import subprocess

    calls = []

    def fake_run(args, **kwargs):
        calls.append(args)
        output = "-remove-hyphens" if "-h" in args else "synthetic text"
        return subprocess.CompletedProcess(args, 0, output, "")

    monkeypatch.setattr(bd.subprocess, "run", fake_run)
    assert bd.extract_text(tmp_path / "synthetic.pdf") == "synthetic text"
    assert calls[1][1:7] == ["-layout", "-fixed", "3", "-remove-hyphens", "none",
                              str(tmp_path / "synthetic.pdf")]
