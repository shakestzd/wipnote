# /// script
# requires-python = ">=3.10"
# dependencies = ["textual>=8.2,<9"]  # keep equal to TEXTUAL_REQ in hooks/panescript.ts (a test checks)
# ///
"""A pane view for one Codex run: the live log, then the answer rendered as Markdown.

Reads the run directory written by the codex-herdr-pane mod:
  clean.md   the same run as Markdown (grows while it runs); the Live tab renders it, each
             tool result between fold markers (md.jq) as one collapsible section
  clean.log  the log with noise removed; the Live tab falls back to it when clean.md is absent
  last.md    Codex's final message (written when it finishes)
  usage      the token split, written by the wrapper (from codex.jq)
  exit       the exit code (appears when the run ends)
  start      epoch seconds when the run began
  label      a short title
"""
from __future__ import annotations

import re
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.css.query import NoMatches
from textual.containers import VerticalScroll
from textual.content import Content
from textual.widgets import Collapsible, Footer, Markdown, MarkdownViewer, RichLog, Static, TabbedContent, TabPane

def read(path: Path) -> str:
    try:
        return path.read_text(errors="replace")
    except OSError:
        return ""


def clock(seconds: float) -> str:
    m, s = divmod(int(seconds), 60)
    return f"{m}m{s:02d}s" if m else f"{s}s"


# The fold markers md.jq writes around a tool result, and the CommonMark fence rule of md.jq's
# fence_open / fence_step: a marker counts only outside a fence, so tool output cannot make one.
FOLD_OPEN = re.compile(r"^<!-- fold (open|closed): (.*) -->$")
FOLD_END = "<!-- /fold -->"
FENCE_OPEN = re.compile(r"^ {0,3}(`{3,}|~{3,})(.*)$")
# The call's label (md.jq's toolline), which md.jq writes as the first line of a fold body
LABEL = re.compile(r"^\*\*[A-Za-z0-9_.:-]+\*\*( .*)?$")


def fence_step(state: tuple[str, int] | None, line: str) -> tuple[str, int] | None:
    if state is None:
        m = FENCE_OPEN.match(line)
        if m and (m[1][0] == "~" or "`" not in m[2]):
            return m[1][0], len(m[1])
        return None
    c, n = state
    return None if re.match(rf"^ {{0,3}}{re.escape(c)}{{{n},}}\s*$", line) else state


@dataclass
class Fold:
    title: str
    open: bool
    lines: list[str] = field(default_factory=list)
    fence: tuple[str, int] | None = None


class LiveDoc:
    """Splits the lines of clean.md into prose and folds. feed() takes complete lines and
    returns ("prose", text) and ("fold", Fold) parts in order; a fold is returned once its end
    marker arrives (flush() returns one the run never closed)."""

    def __init__(self) -> None:
        self.fence: tuple[str, int] | None = None
        self.fold: Fold | None = None

    def feed(self, text: str) -> list[tuple[str, object]]:
        parts: list[tuple[str, object]] = []
        prose: list[str] = []
        for line in text.splitlines(keepends=True):
            bare = line.rstrip("\n")
            if self.fold is not None:
                f = self.fold
                if f.fence is None and bare == FOLD_END:
                    parts.append(("fold", f))
                    self.fold = None
                else:
                    f.lines.append(line)
                    f.fence = fence_step(f.fence, bare)
                continue
            m = FOLD_OPEN.match(bare) if self.fence is None else None
            if m:
                if prose:
                    parts.append(("prose", "".join(prose)))
                    prose = []
                self.fold = Fold(m[2], m[1] == "open")
                continue
            prose.append(line)
            self.fence = fence_step(self.fence, bare)
        if prose:
            parts.append(("prose", "".join(prose)))
        return parts

    def flush(self) -> list[tuple[str, object]]:
        f, self.fold = self.fold, None
        return [("fold", f)] if f is not None else []


class CodexView(App):
    CSS = """
    #status { height: 3; padding: 0 1; background: $panel; border-left: thick $warning; }
    #status.ok { border-left: thick $success; }
    #status.fail { border-left: thick $error; }
    RichLog { padding: 0 1; }
    #livemd { display: none; padding: 0 1; }
    #livemd.on { display: block; }
    #livemd Markdown { margin: 0; }
    #livemd Collapsible { margin: 0 0 1 0; padding-bottom: 0; }
    #livemd Collapsible.failed CollapsibleTitle { color: $error; }
    #log.off { display: none; }
    """
    BINDINGS = [
        Binding("q", "quit", "Quit view"),
        Binding("l", "tab('live')", "Live log"),
        Binding("a", "tab('answer')", "Answer"),
        Binding("t", "toc", "Contents"),
        Binding("f", "folds", "Fold results"),
        Binding("c", "copy", "Copy answer"),
    ]

    def __init__(self, run_dir: Path) -> None:
        super().__init__()
        self.dir = run_dir
        self.offset = 0
        self.md_offset = 0  # bytes of clean.md already written to the stream
        self.stream = None  # the MarkdownStream of the prose block being written, if any
        self.doc = LiveDoc()
        self.md_live = False
        self.finished = False
        self.answer = ""
        self.started = float(read(run_dir / "start").strip() or time.time())

    def compose(self) -> ComposeResult:
        yield Static(id="status")
        with TabbedContent(initial="live"):
            with TabPane("Live", id="live"):
                yield RichLog(id="log", wrap=True, markup=False, highlight=False, auto_scroll=True)
                yield VerticalScroll(id="livemd")
            with TabPane("Answer", id="answer"):
                yield MarkdownViewer("*The answer appears here when Codex finishes.*", show_table_of_contents=True, id="md")
        yield Footer()

    async def on_mount(self) -> None:
        self.title = read(self.dir / "label").strip() or "Codex"
        self.query_one("#livemd", VerticalScroll).anchor()  # stays at the end until the user scrolls up
        self.set_interval(0.4, self.tick)
        await self.tick()

    async def follow_md(self) -> bool:
        """Show the new complete lines of clean.md in the Live tab. Prose goes to a Markdown
        widget through Markdown.get_stream(), which batches writes that arrive faster than the
        widget renders and appends only the tail block, so cost follows what is new, not the
        whole log. Each tool result (between fold markers) becomes its own Collapsible, closed
        unless it failed, and the prose after it starts a new Markdown widget below it.
        A trailing partial line waits for its newline; the writer emits whole blocks, so
        what is written is whole blocks except between lines of one."""
        path = self.dir / "clean.md"
        try:
            size = path.stat().st_size
        except OSError:
            return False
        box = self.query_one("#livemd", VerticalScroll)
        if not self.md_live:
            self.md_live = True
            box.add_class("on")
            self.query_one("#log").add_class("off")
        if size < self.md_offset:  # the file was replaced by something shorter: start over
            await self.stop_stream()
            await box.remove_children()
            self.doc = LiveDoc()
            self.md_offset = 0
        if size > self.md_offset:
            with path.open("rb") as f:
                f.seek(self.md_offset)
                chunk = f.read()
            whole = chunk[: chunk.rfind(b"\n") + 1]
            if whole:
                self.md_offset += len(whole)
                await self.show(self.doc.feed(whole.decode(errors="replace")))
        return True

    async def show(self, parts: list[tuple[str, object]]) -> None:
        box = self.query_one("#livemd", VerticalScroll)
        for kind, part in parts:
            if kind == "prose":
                if self.stream is None:
                    md = Markdown("", classes="prose")
                    await box.mount(md)
                    self.stream = Markdown.get_stream(md)
                await self.stream.write(part)
            else:
                await self.stop_stream()
                await box.mount(self.fold_widget(part))

    @staticmethod
    def fold_body(f: Fold) -> str:
        """The fold's Markdown without the call's label, which repeats the title. A title that
        was cut ("...") keeps the label, the only place the whole command is shown."""
        lines = f.lines
        if lines and "..." not in f.title and LABEL.match(lines[0].rstrip("\n")):
            lines = lines[1:]
        return "".join(lines).strip("\n") or "*(empty)*"

    @classmethod
    def fold_widget(cls, f: Fold) -> Collapsible:
        # Content, not a str: the title is the command, so markup in it must stay literal
        return Collapsible(Markdown(cls.fold_body(f)), title=Content(f.title),
                           collapsed=not f.open, classes="failed" if f.open else "")

    def live_text(self) -> str:
        """Everything the Live tab shows, prose and fold bodies, in order (for tests)."""
        out = []
        for w in self.query_one("#livemd", VerticalScroll).children:
            if isinstance(w, Markdown):
                out.append(w.source)
            elif isinstance(w, Collapsible):
                out.append("".join(m.source for m in w.query(Markdown)))
        return "".join(out)

    async def stop_stream(self) -> None:
        if self.stream is not None:
            stream, self.stream = self.stream, None
            await stream.stop()

    async def tick(self) -> None:
        try:
            await self.step()
        except NoMatches:  # the app is closing and its widgets are gone (a tick still in flight)
            pass

    async def step(self) -> None:
        if not await self.follow_md():
            self.follow_log()
        code = read(self.dir / "exit").strip()
        if code and not self.finished:
            self.finished = True
            await self.show(self.doc.flush())
            await self.stop_stream()
            await self.finish(int(code or 1))
        self.paint(code)

    def follow_log(self) -> None:
        raw = (self.dir / "clean.log")
        try:
            with raw.open("rb") as f:
                f.seek(self.offset)
                chunk = f.read()
                self.offset += len(chunk)
        except OSError:
            chunk = b""
        if chunk:
            self.query_one("#log", RichLog).write(chunk.decode(errors="replace").rstrip("\n"))

    def paint(self, code: str) -> None:
        status = self.query_one("#status", Static)
        label = self.title
        if not code:
            status.update(f"[b]{label}[/b]\nrunning for {clock(time.time() - self.started)}")
            return
        took = clock(float(read(self.dir / "end").strip() or time.time()) - self.started)
        tokens = read(self.dir / "usage").strip()
        used = f" · {tokens}" if tokens else ""
        mark = "finished" if code == "0" else f"[b]failed with exit {code}[/b]"
        status.update(f"[b]{label}[/b]\n{mark} in {took}{used}")
        status.set_classes("ok" if code == "0" else "fail")

    async def finish(self, code: int) -> None:
        last = read(self.dir / "last.md").strip()
        if not last:
            log = read(self.dir / "clean.log").strip()
            last = "```text\n" + log[-6000:] + "\n```" if log else "*Codex wrote no answer.*"
        self.answer = last
        await self.query_one("#md", MarkdownViewer).document.update(last)
        if code == 0:
            self.query_one(TabbedContent).active = "answer"

    def action_tab(self, name: str) -> None:
        self.query_one(TabbedContent).active = name

    def action_toc(self) -> None:
        viewer = self.query_one("#md", MarkdownViewer)
        viewer.show_table_of_contents = not viewer.show_table_of_contents

    def action_folds(self) -> None:
        """Opens every tool result in the Live tab, or closes them all when all are open."""
        folds = list(self.query("#livemd Collapsible").results(Collapsible))
        shut = any(c.collapsed for c in folds)
        for c in folds:
            c.collapsed = not shut

    def action_copy(self) -> None:
        if self.answer:
            self.copy_to_clipboard(self.answer)
            self.notify("Answer copied")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: codex_view.py <run directory>")
    CodexView(Path(sys.argv[1])).run()
