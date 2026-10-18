# /// script
# requires-python = ">=3.10"
# dependencies = ["textual>=8.2,<9"]  # keep equal to TEXTUAL_REQ in hooks/panescript.ts (a test checks)
# ///
"""A pane view for one Codex run: the live log, then the answer rendered as Markdown.

Reads the run directory written by the codex-herdr-pane mod:
  clean.md   the same run as Markdown (grows while it runs); the Live tab renders it
  clean.log  the log with noise removed; the Live tab falls back to it when clean.md is absent
  last.md    Codex's final message (written when it finishes)
  usage      the token split, written by the wrapper (from codex.jq)
  exit       the exit code (appears when the run ends)
  start      epoch seconds when the run began
  label      a short title
"""
from __future__ import annotations

import sys
import time
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.css.query import NoMatches
from textual.containers import VerticalScroll
from textual.widgets import Footer, Markdown, MarkdownViewer, RichLog, Static, TabbedContent, TabPane

def read(path: Path) -> str:
    try:
        return path.read_text(errors="replace")
    except OSError:
        return ""


def clock(seconds: float) -> str:
    m, s = divmod(int(seconds), 60)
    return f"{m}m{s:02d}s" if m else f"{s}s"


class CodexView(App):
    CSS = """
    #status { height: 3; padding: 0 1; background: $panel; border-left: thick $warning; }
    #status.ok { border-left: thick $success; }
    #status.fail { border-left: thick $error; }
    RichLog { padding: 0 1; }
    #livemd { display: none; padding: 0 1; }
    #livemd.on { display: block; }
    #log.off { display: none; }
    """
    BINDINGS = [
        Binding("q", "quit", "Quit view"),
        Binding("l", "tab('live')", "Live log"),
        Binding("a", "tab('answer')", "Answer"),
        Binding("t", "toc", "Contents"),
        Binding("c", "copy", "Copy answer"),
    ]

    def __init__(self, run_dir: Path) -> None:
        super().__init__()
        self.dir = run_dir
        self.offset = 0
        self.md_offset = 0  # bytes of clean.md already written to the stream
        self.stream = None  # the MarkdownStream, created on the first write
        self.md_live = False
        self.finished = False
        self.answer = ""
        self.started = float(read(run_dir / "start").strip() or time.time())

    def compose(self) -> ComposeResult:
        yield Static(id="status")
        with TabbedContent(initial="live"):
            with TabPane("Live", id="live"):
                yield RichLog(id="log", wrap=True, markup=False, highlight=False, auto_scroll=True)
                with VerticalScroll(id="livemd"):
                    yield Markdown("", id="live_md")
            with TabPane("Answer", id="answer"):
                yield MarkdownViewer("*The answer appears here when Codex finishes.*", show_table_of_contents=True, id="md")
        yield Footer()

    async def on_mount(self) -> None:
        self.title = read(self.dir / "label").strip() or "Codex"
        self.query_one("#livemd", VerticalScroll).anchor()  # stays at the end until the user scrolls up
        self.set_interval(0.4, self.tick)
        await self.tick()

    async def follow_md(self) -> bool:
        """Stream the new complete lines of clean.md into the Live tab's Markdown widget.
        Markdown.get_stream() batches writes that arrive faster than the widget renders
        and appends only the tail block, so cost follows what is new, not the whole log.
        A trailing partial line waits for its newline; the writer emits whole blocks, so
        what is written is whole blocks except between lines of one."""
        path = self.dir / "clean.md"
        try:
            size = path.stat().st_size
        except OSError:
            return False
        md = self.query_one("#live_md", Markdown)
        if not self.md_live:
            self.md_live = True
            self.query_one("#livemd").add_class("on")
            self.query_one("#log").add_class("off")
        if size < self.md_offset:  # the file was replaced by something shorter: start over
            await self.stop_stream()
            await md.update("")
            self.md_offset = 0
        if size > self.md_offset:
            with path.open("rb") as f:
                f.seek(self.md_offset)
                chunk = f.read()
            whole = chunk[: chunk.rfind(b"\n") + 1]
            if whole:
                self.md_offset += len(whole)
                if self.stream is None:
                    self.stream = Markdown.get_stream(md)
                await self.stream.write(whole.decode(errors="replace"))
        return True

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

    def action_copy(self) -> None:
        if self.answer:
            self.copy_to_clipboard(self.answer)
            self.notify("Answer copied")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: codex_view.py <run directory>")
    CodexView(Path(sys.argv[1])).run()
