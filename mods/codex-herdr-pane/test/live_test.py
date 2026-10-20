"""Headless Textual test of the Live tab (Markdown via MarkdownStream, RichLog fallback).

Run: uv run --no-project --with 'textual>=8.2,<9' python test/live_test.py
"""
import asyncio
import sys
import tempfile
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "viewer"))
from codex_view import CodexView  # noqa: E402
from textual.containers import VerticalScroll  # noqa: E402
from textual.widgets import Collapsible, Markdown, RichLog  # noqa: E402

FAILS: list[str] = []


def check(name: str, cond: bool, detail: str = "") -> None:
    print(("ok   " if cond else "FAIL ") + name + ("" if cond else f"  [{detail}]"))
    if not cond:
        FAILS.append(name)


def src(app: CodexView) -> str:
    return app.live_text()


def folds(app: CodexView) -> list[Collapsible]:
    return list(app.query("#livemd Collapsible").results(Collapsible))


def newrun() -> Path:
    d = Path(tempfile.mkdtemp())
    (d / "start").write_text(str(int(time.time())))
    return d


async def growing() -> None:
    d = newrun()
    (d / "clean.md").write_text("### Turn\n\n")
    app = CodexView(d)
    async with app.run_test(size=(90, 24)) as pilot:
        await pilot.pause(0.9)
        check("md: first block streamed", src(app).startswith("### Turn"), src(app))
        check("md: Live shows markdown, not the log", app.query_one("#livemd").has_class("on") and app.query_one("#log").has_class("off"))
        with (d / "clean.md").open("a") as f:
            f.write("**Bash** `ls`\n\n```\nline one\nlin")  # chunk boundary mid-line, inside a fence
        await pilot.pause(0.9)
        check("md: partial trailing line held back", "lin" not in src(app).replace("line one", ""), repr(src(app)))
        check("md: complete lines before it written", "line one" in src(app), repr(src(app)))
        with (d / "clean.md").open("a") as f:
            f.write("e two\n```\n\n")
        await pilot.pause(0.9)
        check("md: held line completed once its newline arrived", "line two" in src(app) and src(app).count("```") == 2, repr(src(app)))
        size = app.md_offset
        await pilot.pause(0.9)
        check("md: nothing re-written when the file did not change", app.md_offset == size and app.stream is not None)
        scroll = app.query_one("#livemd", VerticalScroll)
        with (d / "clean.md").open("a") as f:
            f.write("".join(f"### Assistant\n\nparagraph {i}\n\n" for i in range(60)))
        await pilot.pause(1.2)
        check("md: anchored to the end while it grows", scroll.max_scroll_y > 0 and scroll.scroll_y >= scroll.max_scroll_y - 1, f"{scroll.scroll_y}/{scroll.max_scroll_y}")
        scroll.scroll_to(y=0, animate=False)
        scroll.release_anchor()
        await pilot.pause(0.3)
        with (d / "clean.md").open("a") as f:
            f.write("".join(f"### Assistant\n\nmore {i}\n\n" for i in range(30)))
        await pilot.pause(1.2)
        check("md: stays put when the user scrolled up", scroll.scroll_y < 3 and scroll.max_scroll_y > 10, f"{scroll.scroll_y}/{scroll.max_scroll_y}")
        check("md: all text arrived", "more 29" in src(app))
        note = "*macOS xcrun cache errors hidden (sandbox), see raw.log*"
        with (d / "clean.md").open("a") as f:
            f.write(f"**Bash** `git status`\n\n```\n(no output)\n```\n\n{note}\n\n*MCP server unreachable, see raw.log*\n\n")
        await pilot.pause(1.2)
        check("md: the xcrun and MCP notes stream through as written, once each", src(app).count(note) == 1 and src(app).count("*MCP server unreachable") == 1, src(app)[-200:])
        (d / "end").write_text(str(int(time.time())))
        (d / "exit").write_text("0")
        await pilot.pause(1.0)
        check("md: finish stops the stream", app.stream is None and app.finished)


async def fallback() -> None:
    d = newrun()
    (d / "clean.log").write_text("> echo hi\n")
    app = CodexView(d)
    async with app.run_test(size=(90, 24)) as pilot:
        await pilot.pause(0.9)
        check("fallback: RichLog shown, markdown hidden", not app.query_one("#log").has_class("off") and not app.query_one("#livemd").has_class("on"))
        check("fallback: log lines written", len(app.query_one("#log", RichLog).lines) == 1, str(len(app.query_one("#log", RichLog).lines)))
        (d / "clean.md").write_text("### Turn\n\n")
        await pilot.pause(0.9)
        check("fallback: switches to markdown when clean.md appears", app.query_one("#livemd").has_class("on") and "Turn" in src(app))


async def folded() -> None:
    d = newrun()
    ok_fold = "<!-- fold closed: Bash: ls [b]x[/b] · exit 0 · 2 lines -->\n**Bash** `ls [b]x[/b]`\n\n```\na\nb\n```\n<!-- /fold -->\n\n"
    (d / "clean.md").write_text("### Assistant\n\nI will list the files.\n\n" + ok_fold)
    app = CodexView(d)
    async with app.run_test(size=(90, 30)) as pilot:
        await pilot.pause(0.9)
        fs = folds(app)
        check("fold: a tool result becomes one collapsible", len(fs) == 1, str(len(fs)))
        check("fold: a passing result starts closed", fs and fs[0].collapsed)
        check("fold: the title is the marker's, markup kept literal", fs and "[b]x[/b]" in str(fs[0].title), fs and str(fs[0].title))
        check("fold: the markers are not shown", "<!--" not in src(app), src(app))
        check("fold: the label that repeats the title is hidden", "**Bash**" not in src(app) and "a\nb" in src(app), src(app))
        check("fold: the prose before it is its own block", "I will list the files." in src(app))
        with (d / "clean.md").open("a") as f:  # a fold split across two reads, a marker inside its fence
            f.write("<!-- fold open: Bash: cat x · exit 1 · 3 lines -->\n> **error** exit 1\n\n````\n<!-- /fold -->\n")
        await pilot.pause(0.9)
        check("fold: an unfinished fold waits for its end marker", len(folds(app)) == 1, str(len(folds(app))))
        with (d / "clean.md").open("a") as f:
            f.write("still output\n````\n<!-- /fold -->\n\n### Assistant\n\nThe file is missing.\n\n")
        await pilot.pause(0.9)
        fs = folds(app)
        check("fold: a marker inside the result's fence is output, not the end", len(fs) == 2 and "still output" in src(app) and src(app).count("<!-- /fold -->") == 1, src(app))
        check("fold: a failed result starts open and is marked", len(fs) == 2 and not fs[1].collapsed and fs[1].has_class("failed"))
        kids = list(app.query_one("#livemd", VerticalScroll).children)
        check("fold: prose after a fold goes below it", isinstance(kids[-1], Markdown) and "The file is missing." in kids[-1].source, str(kids))
        app.action_folds()
        await pilot.pause(0.2)
        check("fold: f opens every result", all(not c.collapsed for c in folds(app)))
        app.action_folds()
        await pilot.pause(0.2)
        check("fold: f again closes them all", all(c.collapsed for c in folds(app)))
        with (d / "clean.md").open("a") as f:  # a marker inside a prose fence stays prose
            f.write("### Assistant\n\n```\n<!-- fold closed: fake -->\n```\n\n")
        await pilot.pause(0.9)
        check("fold: a marker inside a prose fence is not a fold", len(folds(app)) == 2 and "fake" in src(app))
        with (d / "clean.md").open("a") as f:  # a cut title keeps the label: it holds the whole command
            f.write("<!-- fold closed: Bash: long... · exit 0 · 1 line -->\n**Bash** `long command in full`\n\n```\ny\n```\n<!-- /fold -->\n\n")
        await pilot.pause(0.9)
        check("fold: a cut title keeps the label", "long command in full" in src(app), src(app)[-200:])
        with (d / "clean.md").open("a") as f:
            f.write("<!-- fold closed: never closed -->\n```\nz\n```\n")
        (d / "end").write_text(str(int(time.time())))
        (d / "exit").write_text("1")
        await pilot.pause(1.0)
        check("fold: one the run never closed is shown when it finishes", len(folds(app)) == 4, str(len(folds(app))))
        (d / "clean.md").write_text("### Turn\n\n")
        await pilot.pause(0.9)
        check("fold: a shorter replacement starts the view over", not folds(app) and src(app).startswith("### Turn"), src(app))


async def main() -> None:
    await growing()
    await fallback()
    await folded()
    print(f"\n{len(FAILS)} failed" if FAILS else "\nlive tab tests passed")
    sys.exit(1 if FAILS else 0)


asyncio.run(main())
