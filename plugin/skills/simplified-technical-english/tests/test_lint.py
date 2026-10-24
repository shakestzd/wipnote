"""Linter tests. Limits are read from ste_lint, and each is tested just under and just over."""

import re
import subprocess
from pathlib import Path

import pytest
import ste_lint as sl
from ste_segment import count_words

SCRIPT = Path(sl.__file__)


def words(n: int) -> str:
    """A sentence of exactly n plain words."""
    return " ".join(["Unit"] + ["unit"] * (n - 1)) + "."


def rules(text: str, mode: str = "auto") -> list[str]:
    return [f.rule for f in sl.lint(text, mode)]


# ---- sentence length (Rules 5.1, 5.5, 6.3, 8.4) --------------------------------------

@pytest.mark.parametrize("prefix,limit,rule", [
    ("1. ", sl.MAX_WORDS_PROCEDURE, "5.1"),
    ("", sl.MAX_WORDS_DESCRIPTIVE, "6.3"),
    ("NOTE: ", sl.MAX_WORDS_NOTE, "5.5"),
    ("WARNING: ", sl.MAX_WORDS_SAFETY, "5.1"),
])
def test_length_limit_is_two_sided(prefix, limit, rule):
    assert rules(prefix + words(limit)) == []
    assert rules(prefix + words(limit + 1)) == [rule]


def test_forced_procedure_mode_uses_the_procedure_limit():
    over = words(sl.MAX_WORDS_PROCEDURE + 1)
    assert rules(over, "procedure") == ["5.1"]
    assert rules(over, "descriptive") == []


def test_each_sentence_is_counted_on_its_own():
    text = words(sl.MAX_WORDS_DESCRIPTIVE) + " " + words(sl.MAX_WORDS_DESCRIPTIVE)
    assert rules(text) == []


def test_finding_position_points_at_the_sentence_start():
    text = "Short one.\n" + words(sl.MAX_WORDS_DESCRIPTIVE + 1)
    finding = sl.lint(text)[0]
    assert (finding.line, finding.col) == (2, 1)


def test_vertical_list_items_count_as_separate_sentences():
    intro = "These parts are in the kit:"
    items = "\n".join(f"- {words(sl.MAX_WORDS_DESCRIPTIVE)}" for _ in range(3))
    assert rules(f"{intro}\n{items}") == []
    assert rules(f"{intro}\n- {words(sl.MAX_WORDS_DESCRIPTIVE + 1)}") == ["6.3"]


def test_procedure_list_items_use_the_procedure_limit():
    text = f"1. Do these:\n- {words(sl.MAX_WORDS_PROCEDURE + 1)}"
    assert rules(text) == ["5.1"]


# ---- word counting (Rules 8.5, 8.6, 8.7) ----------------------------------------------

def test_parenthesised_text_counts_as_one_word():
    limit = sl.MAX_WORDS_DESCRIPTIVE
    inner = "(this long note has many words in it)"
    assert rules(" ".join(["unit"] * (limit - 1)) + f" {inner}.") == []
    assert rules(" ".join(["unit"] * limit) + f" {inner}.") == ["6.3"]


def test_quoted_text_counts_as_one_word():
    assert count_words("Touch “A very long quoted label” now".replace(
        "“A very long quoted label”", "\x00" * 10)) == 3


def test_number_with_unit_hyphen_word_and_identifier_are_one_word_each():
    assert count_words("Set 10 mm") == 2
    assert count_words("Use the trial-and-error method") == 4
    assert count_words("Examine the No. 1 bearing") == 4
    assert count_words("Tag circuit breaker 36L7") == 4


def test_quoted_text_is_not_counted_in_a_long_sentence():
    limit = sl.MAX_WORDS_DESCRIPTIVE
    quote = '"' + " ".join(["label"] * 30) + '"'
    assert rules(" ".join(["unit"] * (limit - 1)) + f" {quote}.") == []


# ---- paragraph length (Rule 6.6) ------------------------------------------------------

def test_paragraph_sentence_limit_is_two_sided():
    sentence = "The unit is on."
    ok = " ".join([sentence] * sl.MAX_SENTENCES_PER_PARAGRAPH)
    over = " ".join([sentence] * (sl.MAX_SENTENCES_PER_PARAGRAPH + 1))
    assert rules(ok) == []
    assert rules(over) == ["6.6"]


def test_blank_line_starts_a_new_paragraph():
    sentence = "The unit is on."
    block = " ".join([sentence] * sl.MAX_SENTENCES_PER_PARAGRAPH)
    assert rules(block + "\n\n" + block) == []


# ---- punctuation and word forms -------------------------------------------------------

@pytest.mark.parametrize("bad,rule", [
    ("Examine the part; replace it.", "8.1"),
    ("The unit doesn't work.", "4.2"),
    ("It's on.", "4.2"),
    ("The operator has adjusted the linkage.", "3.4"),
    ("The cover was removed by the operator.", "3.6"),
    ("Do not use tools, e.g. a hammer.", "GR-6"),
    ("The user can push his button.", "GR-3"),
    ("Move the main-gear-door-retraction handle.", "2.2"),
    ("Clean the colour panel.", "1.14"),
    ("Look for leaks before removing the cover.", "3.5"),
])
def test_rule_fires_on_bad_input(bad, rule):
    assert rule in rules(bad)


@pytest.mark.parametrize("good", [
    "Examine the part. Replace it.",
    "The unit does not work.",
    "It is on.",
    "The operator adjusted the linkage.",
    "The operator removed the cover.",
    "Do not use tools, for example a hammer.",
    "The user can push the button.",
    "Move the main-gear-door handle.",
    "Clean the color panel.",
    "Look for leaks before you remove the cover.",
    "The unit has a bearing.",
    "The cover is removed.",
])
def test_rule_stays_quiet_on_good_input(good):
    assert rules(good) == []


def test_hyphen_group_limit_is_two_sided():
    ok = "-".join(["part"] * sl.MAX_HYPHEN_GROUP_WORDS)
    over = "-".join(["part"] * (sl.MAX_HYPHEN_GROUP_WORDS + 1))
    assert rules(f"Move the {ok} handle.") == []
    assert rules(f"Move the {over} handle.") == ["2.2"]


def test_quoted_text_is_exempt_from_word_checks():
    assert rules('The label says "Don\'t touch; e.g. he did".') == []


def test_code_fences_and_headings_are_skipped():
    assert rules("# A heading; with a semicolon\n```\nx; y\n```\n") == []


# ---- output and exit status -----------------------------------------------------------

def run_cli(text: str | bytes, *args: str):
    result = subprocess.run(["uv", "run", str(SCRIPT), *args], input=text,
                            text=isinstance(text, str), capture_output=True,
                            check=False, timeout=30)
    if isinstance(result.stdout, bytes):
        result.stdout = result.stdout.decode("utf-8")
        result.stderr = result.stderr.decode("utf-8")
    return result


def assert_read_error(status, out, err):
    assert status == 2
    assert out == ""
    assert len(err.splitlines()) == 1
    assert "error:" in err
    assert "Traceback" not in err


@pytest.mark.parametrize("text,status", [("Examine it.", 0), ("Examine it; stop.", 1)])
def test_dash_reads_stdin_like_the_default(text, status):
    implicit = run_cli(text)
    explicit = run_cli(text, "-")
    assert implicit.returncode == explicit.returncode == status
    assert implicit.stdout == explicit.stdout
    assert implicit.stderr == explicit.stderr == ""


@pytest.mark.parametrize("bad_source", ["file", "stdin"])
def test_dash_can_be_combined_with_files(tmp_path, bad_source):
    path = tmp_path / "sample.txt"
    path.write_text("Examine it.", encoding="utf-8")
    clean = run_cli("Examine it.", str(path), "-")
    assert clean.returncode == 0
    assert clean.stderr == ""
    if bad_source == "file":
        path.write_text("Examine it; stop.", encoding="utf-8")
    text = "Examine it; stop." if bad_source == "stdin" else "Examine it."
    bad = run_cli(text, str(path), "-")
    assert bad.returncode == 1
    assert bad.stderr == ""
    prefix = str(path) if bad_source == "file" else "-"
    assert bad.stdout.startswith(f"{prefix}:1:11  8.1  ")
    assert "# 1 finding(s)." in bad.stdout


@pytest.mark.parametrize("kind", ["missing", "directory"])
def test_cli_file_errors_are_one_line_without_a_traceback(tmp_path, kind):
    path = tmp_path / "sample\nfile.txt"
    path.write_text("Examine it.", encoding="utf-8")
    good = run_cli("", str(path))
    assert good.returncode == 0
    assert good.stderr == ""
    path.unlink()
    if kind == "directory":
        path.mkdir()
    bad = run_cli("", str(path))
    assert_read_error(bad.returncode, bad.stdout, bad.stderr)
    assert repr(str(path)) in bad.stderr


def test_permission_error_is_reported_without_a_traceback(tmp_path, monkeypatch, capsys):
    path = tmp_path / "sample.txt"
    path.write_text("Examine it.", encoding="utf-8")
    assert sl.main([str(path)]) == 0
    good = capsys.readouterr()
    assert "# 0 finding(s)." in good.out
    assert good.err == ""

    def deny_read(self, *args, **kwargs):
        raise PermissionError(13, "Permission denied", str(self))

    monkeypatch.setattr(Path, "read_text", deny_read)
    status = sl.main([str(path)])
    bad = capsys.readouterr()
    assert_read_error(status, bad.out, bad.err)
    assert "Permission denied" in bad.err
    assert str(path) in bad.err


@pytest.mark.parametrize("source", ["implicit", "dash", "file"])
def test_input_requires_valid_utf8(tmp_path, source):
    args = ("-",) if source == "dash" else ()
    good_bytes = "Examine the café panel.\n".encode("utf-8")
    bad_bytes = b"Examine the caf\xe9 panel.\n"
    if source == "file":
        path = tmp_path / "sample.txt"
        path.write_bytes(good_bytes)
        args = (str(path),)
    good = run_cli(good_bytes, *args)
    assert good.returncode == 0
    assert good.stderr == ""
    if source == "file":
        path.write_bytes(bad_bytes)
    bad = run_cli(bad_bytes, *args)
    assert_read_error(bad.returncode, bad.stdout, bad.stderr)
    assert "utf-8" in bad.stderr


@pytest.mark.parametrize("text", ["", " \t\n\r\n", "Examine it", "x" * 100000])
def test_cli_edge_inputs_remain_clean(text, tmp_path):
    path = tmp_path / "sample.txt"
    path.write_text(text, encoding="utf-8")
    for args in ((), ("-",), (str(path),)):
        result = run_cli(text, *args)
        assert result.returncode == 0
        assert result.stdout.startswith("# 0 finding(s).\n")
        assert result.stderr == ""


@pytest.mark.parametrize("ending", ["", "."])
def test_unterminated_and_long_lines_still_check_word_limits(ending):
    for size, status in ((sl.MAX_WORDS_DESCRIPTIVE, 0),
                         (sl.MAX_WORDS_DESCRIPTIVE + 1, 1), (10000, 1)):
        text = " ".join(["unit"] * size) + ending
        result = run_cli(text, "-")
        assert result.returncode == status
        assert result.stderr == ""
        assert ("  6.3  " in result.stdout) == bool(status)
        if status:
            assert f"Sentence has {size} words." in result.stdout


def test_cli_exit_code_and_line_format():
    bad = run_cli("Examine it; replace it.\n")
    assert bad.returncode == 1
    assert re.match(r"^1:\d+  8\.1  ", bad.stdout.splitlines()[0])
    assert run_cli("Examine it.\n").returncode == 0


def test_cli_footer_says_it_checks_only_a_subset():
    out = run_cli("Examine it.\n").stdout
    assert "Subset check only" in out
    assert "does not mean the text complies" in out


def test_disclosure_is_printed_from_the_one_constant():
    result = run_cli("", "--print-disclosure", "/nonexistent/ste-lint-input")
    assert result.returncode == 0
    assert result.stderr == ""
    assert result.stdout.strip() == sl.DISCLOSURE


def test_show_limits_prints_every_constant_from_the_module():
    result = run_cli("", "--show-limits", "/nonexistent/ste-lint-input")
    assert result.returncode == 0
    assert result.stderr == ""
    out = result.stdout.splitlines()
    assert out == sl.limits_lines()
    assert f"MAX_WORDS_PROCEDURE = {sl.MAX_WORDS_PROCEDURE}" in out


# ---- DRY: one place for limits and for the disclosure ---------------------------------

def constants_block() -> list[str]:
    src = SCRIPT.read_text(encoding="utf-8").splitlines()
    return [line for line in src if re.match(r"^MAX_[A-Z_]+\s*=", line)]


def test_every_limit_constant_names_its_rule():
    lines = constants_block()
    assert len(lines) >= 5
    for line in lines:
        assert re.search(r"#.*Rule \d+\.\d+", line), line


def test_limit_numbers_appear_only_in_the_constants_block():
    src = SCRIPT.read_text(encoding="utf-8")
    body = "\n".join(line for line in src.splitlines() if not re.match(r"^MAX_", line))
    for value in (sl.MAX_WORDS_PROCEDURE, sl.MAX_WORDS_DESCRIPTIVE):
        code = re.sub(r"#.*", "", body)
        assert not re.search(rf"(?<![\w.]){value}(?![\w.])", code)


SKILL_DIR = Path(__file__).resolve().parent.parent
DOCS = [SKILL_DIR / "SKILL.md", SKILL_DIR / "README.md",
        *(SKILL_DIR / "references").glob("*.md")]


def test_docs_do_not_restate_the_script_limits():
    forbidden = re.compile(
        rf"\b(?:{sl.MAX_WORDS_PROCEDURE}|{sl.MAX_WORDS_DESCRIPTIVE}|six)\b", re.IGNORECASE)
    for doc in DOCS:
        if doc.exists():
            assert not forbidden.search(doc.read_text(encoding="utf-8")), doc.name


def test_skill_md_quotes_the_disclosure_exactly_once():
    skill = (SKILL_DIR / "SKILL.md").read_text(encoding="utf-8")
    assert skill.count(sl.DISCLOSURE) == 1


def test_no_spec_or_dictionary_file_ships_outside_source():
    for path in SKILL_DIR.rglob("*"):
        if path.is_file() and "source" not in path.relative_to(SKILL_DIR).parts:
            assert path.suffix.lower() not in {".pdf", ".sqlite3", ".db"}, path


# ---- false-positive guards for 1.14 (British spelling) and 3.4 (perfect tense) ------

@pytest.mark.parametrize("text", [
    "The cancellation of the organism test.",
    "The organization reviewed the organism.",
    "The reorganization plan lists each organism.",
    "The traveler reads the manual.",
])
def test_us_spellings_do_not_hit_british_rule(text):
    assert "1.14" not in rules(text)


@pytest.mark.parametrize("text", [
    "Keep the seal. You have need of the tool.",
    "The table has seed data.",
    "We have feed the unit and have speed it up.",
    "They had indeed proceed past the bed.",
])
def test_non_participle_ed_words_do_not_hit_perfect_tense(text):
    assert "3.4" not in rules(text)


@pytest.mark.parametrize("text,rule", [
    ("Operators organised the parts.", "1.14"),
    ("The team has organisation charts.", "1.14"),
    ("They utilise the tool.", "1.14"),
    ("The operator cancelled the job.", "1.14"),
    ("The engineer has travelled to site.", "3.4"),
    ("The crew have finished the task.", "3.4"),
    ("You have need of the tool.", None),
])
def test_real_british_and_perfect_tense_still_hit(text, rule):
    found = rules(text)
    if rule is None:
        assert "3.4" not in found
    else:
        assert rule in found
