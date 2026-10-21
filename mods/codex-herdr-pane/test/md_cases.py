"""Behavioural tests of viewer/codex.jq and viewer/stream.jq in text and md mode.

Run: uv run --no-project --with 'textual>=8.2,<9' python test/md_cases.py
(textual brings markdown-it-py, used here as an independent CommonMark reader.)
"""
import json
import re
import subprocess
import sys
from pathlib import Path

from markdown_it import MarkdownIt

V = Path(__file__).resolve().parent.parent / "viewer"
FAILS: list[str] = []


def md_codex(lines: list[str]) -> str:
    """What the pane's mdcodex does: clean.awk's noise rules, then codex.jq in md mode."""
    awk = subprocess.run(["awk", "-f", str(V / "clean.awk")], input="\n".join(lines) + "\n", capture_output=True, text=True, check=True)
    return jq("codex.jq", "md", awk.stdout.splitlines())


def cmd_item(i: int, command: str, event: str, output: str = "", code: int | None = 0) -> str:
    return json.dumps({"type": event, "item": {"id": f"item_{i}", "type": "command_execution", "command": command,
                                              "aggregated_output": output, "exit_code": code if event == "item.completed" else None}})


def jq(script: str, mode: str, lines: list[str]) -> str:
    stateful = script == "stream.jq" and mode.startswith("md")  # pairs calls with results, so it reads with inputs
    out = subprocess.run(["jq", "--unbuffered", "-R", "-r", *(["-n"] if stateful else []), "--arg", "mode", mode, "-f", str(V / script)],
                         input="\n".join(lines) + "\n", capture_output=True, text=True, check=True)
    return out.stdout


def check(name: str, cond: bool, detail: str = "") -> None:
    print(("ok   " if cond else "FAIL ") + name)
    if not cond:
        FAILS.append(name)
        print("     " + detail.replace("\n", "\n     "))


def item(kind: str, event: str, **kw) -> str:
    return json.dumps({"type": event, "item": {"id": "i", "type": kind, **kw}})


def done(n: int = 5) -> str:
    return json.dumps({"type": "turn.completed", "usage": {"input_tokens": 13866, "cached_input_tokens": 0, "output_tokens": n}})


FOLD = re.compile(r"^<!-- fold (?:open|closed): [^\n]* -->\n|^<!-- /fold -->\n", re.M)


def unfold(md: str) -> str:
    """md without the fold markers around each tool result (the Live tab's collapsible sections)."""
    return FOLD.sub("", md)


def fences(md: str) -> list[tuple[str, str]]:
    """(fence line, body) per fenced block according to markdown-it."""
    return [(t.markup, t.content) for t in MarkdownIt("gfm-like").parse(md) if t.type == "fence"]


def kinds(md: str) -> list[str]:
    return [t.type for t in MarkdownIt("gfm-like").parse(md)]


def run_event(output: str, code: int = 0) -> str:
    return jq("codex.jq", "md", [item("command_execution", "item.started", command="cat f"),
                                 item("command_execution", "item.completed", command="cat f", aggregated_output=output, exit_code=code)])


def hostile() -> None:
    body = "```\n`````\n# not a heading\n---\n| a | b |\n|---|---|\n<script>alert(1)</script>\n> quote\n- item\n~~~\n    ```"
    md = run_event(body)
    fs = fences(md)
    check("hostile: exactly one fence block", len(fs) == 1, md)
    mark, content = fs[0]
    check("hostile: fence longer than the inner run of 5", len(mark) >= 6, mark)
    check("hostile: the parsed block holds the whole output", content.rstrip("\n") == body, repr(content))
    opens = [l for l in md.splitlines() if re.fullmatch(r"`{6,}", l)]
    check("hostile: opening and closing fence lines match", len(opens) == 2 and opens[0] == opens[1], md)
    inner_max = max(len(m) for m in re.findall(r"`+", body))
    closers = [l for l in body.splitlines() if re.fullmatch(r" {0,3}`{%d,}\s*" % len(mark), l)]
    check("hostile: no inner line can close the fence", not closers and len(mark) > inner_max)
    ks = kinds(md)
    bad = {"heading_open", "table_open", "hr", "html_block", "blockquote_open", "bullet_list_open"} & set(kinds(unfold(md)))
    check("hostile: nothing injected outside the fence", not bad, str(ks))
    check("hostile: the only HTML is the two fold markers", ks.count("html_block") == 2 and ks[0] == ks[-1] == "html_block", str(ks))
    check("hostile: the call is a bold label", unfold(md).startswith("**Bash** `cat f`\n"), md)


def tilde_and_ticks() -> None:
    for n in (0, 2, 3, 7):
        body = ("`" * n) + "x" if n else "plain"
        mark, content = fences(run_event(body))[0]
        check(f"fence for a run of {n} backticks is at least 3 and longer", len(mark) >= max(3, n + 1) and content.strip() == body, mark)


def normal() -> None:
    lines = [json.dumps({"type": "turn.started"}), item("agent_message", "item.completed", text="Working on it."),
             item("command_execution", "item.started", command="ls -la"),
             item("command_execution", "item.completed", command="ls -la", aggregated_output="a\nb\n", exit_code=0),
             done()]
    md = jq("codex.jq", "md", lines)
    check("normal: turn heading", "### Turn\n" in md, md)
    check("normal: assistant heading and prose", "### Assistant\n\nWorking on it.\n" in md, md)
    check("normal: tool label", "**Bash** `ls -la`\n" in md, md)
    check("normal: result fenced", fences(md) == [("```", "a\nb\n")], str(fences(md)))
    check("normal: footer", md.rstrip("\n").endswith("*input 13,866 (cached 0), output 5*"), md)
    check("normal: no error quote", "> **error**" not in md)
    text = jq("codex.jq", "view", lines)
    check("normal: text mode unchanged", text == "Working on it.\n> ls -la\n  = exit 0: a b \n--- done: input 13,866 (cached 0), output 5 ---\n", text)


def truncated() -> None:
    md = run_event("x" * 5000)
    mark, content = fences(md)[0]
    check("truncated: cut and says so", len(content) < 1700 and "[truncated, 3500 more characters]" in content, str(len(content)))
    check("truncated: still one closed fence", md.count(mark) == 2)
    text = jq("codex.jq", "view", [item("command_execution", "item.completed", exit_code=0, aggregated_output="y" * 500)])
    check("truncated: text mode still clips at 200", text.endswith("y" * 200 + "...\n"), text[-30:])


def errors() -> None:
    md = run_event("boom `x`", code=2)
    check("error: leading error quote with exit", unfold(md).startswith("**Bash**") and "\n> **error** exit 2\n\n```" in md, md)
    check("error: the output is still fenced", fences(md)[0][1].strip() == "boom `x`", md)
    ev = jq("codex.jq", "md", [json.dumps({"type": "error", "message": "bad *thing* # [x](y) <b>"})])
    check("error: event message is escaped, one quote line", ev.startswith("> **error** bad \\*thing\\* \\# \\[x\\](y) \\<b\\>\n"), ev)
    check("error: parsed as a blockquote only", {"heading_open", "html_block"}.isdisjoint(kinds(ev)) and "blockquote_open" in kinds(ev), str(kinds(ev)))
    sm = jq("stream.jq", "md", [json.dumps({"type": "user", "message": {"content": [{"type": "tool_result", "content": "nope", "is_error": True}]}})])
    check("error: stream tool error flagged", unfold(sm).startswith("> **error**\n\n```\nnope\n```"), sm)


def passthrough_and_prose() -> None:
    md = jq("codex.jq", "md", ["Reading additional input from stdin...", "```", "", "# h"])
    ks = [f[1] for f in fences(md)]
    check("passthrough: other lines each a text fence, blank skipped, the stdin line dropped", ks == ["```\n", "# h\n"], str(ks))
    check("passthrough: the stdin line is gone entirely", "Reading additional" not in md, md)
    check("passthrough: info string is text", "```text\n" in md)
    raw = jq("codex.jq", "mdraw", [item("agent_message", "item.completed", text="x")])
    check("mdraw: JSON line is not rendered as an event", len(fences(raw)) == 1 and fences(raw)[0][1].startswith('{"type": "item.completed"'), raw)
    open_ = jq("codex.jq", "md", [item("agent_message", "item.completed", text="before\n```py\nunclosed")])
    after = open_ + jq("codex.jq", "md", [done()])
    check("prose: an unclosed fence in assistant text is closed in its own block",
          len(fences(open_)) == 1 and fences(after)[0][1] == "unclosed\n" and kinds(after).count("fence") == 1 and "*input" in after.split("```")[-1], after)
    check("prose: footer after unclosed fence stays prose", "paragraph_open" in kinds(after))


def partial() -> None:
    full = [item("command_execution", "item.completed", command="x", aggregated_output="a```b", exit_code=0), done()]
    cut = full[0][: len(full[0]) // 2]
    for name, lines in (("stream cut mid-line", [full[0], cut]), ("stream cut before final event", [full[0]]), ("empty", [])):
        md = jq("codex.jq", "md", lines) if lines else ""
        ticks = [l for l in md.splitlines() if l.startswith("`")]
        check(f"partial: {name}: no dangling fence", len(ticks) % 2 == 0, md)
    md = jq("codex.jq", "md", [full[0], cut])
    check("partial: the cut line is shown as plain text, the earlier block intact", len(fences(md)) == 2 and fences(md)[0][1] == "a```b\n", md)
    sm = jq("stream.jq", "md", ['{"type":"assistant","message":{"content":[{"type":"te'])
    check("partial: stream.jq skips an incomplete line", sm == "", repr(sm))


def stream_normal() -> None:
    lines = [json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": "hello"}, {"type": "tool_use", "name": "Bash", "input": {"command": "echo `date`"}}]}}),
             json.dumps({"type": "user", "message": {"content": [{"type": "tool_result", "content": [{"text": "out ``` x"}], "is_error": False}]}}),
             json.dumps({"type": "result", "subtype": "success", "duration_ms": 3000, "num_turns": 2, "usage": {"input_tokens": 5, "output_tokens": 7}, "total_cost_usd": 0.01})]
    md = jq("stream.jq", "md", lines)
    check("stream: heading, prose, label with backticks", "### Assistant\n\nhello\n" in md and "**Bash** `` echo `date` ``" in md, md)
    check("stream: result fenced longer than inner run", fences(md) == [("````", "out ``` x\n")], str(fences(md)))
    check("stream: footer", md.rstrip("\n").endswith("*success in 3s, 2 turns, 5 in / 7 out tokens, $0.01*"), md)
    text = jq("stream.jq", "view", lines)
    check("stream: text mode unchanged", text == "hello\n> Bash: echo `date`\n  = out ``` x\n--- success in 3s, 2 turns, 5 in / 7 out tokens, $0.01 ---\n", text)


def label_blocks(md: str) -> list[tuple[str, str]]:
    """(label line, fenced body) for each label paragraph directly followed by a fence."""
    toks = MarkdownIt("gfm-like").parse(md)
    out = []
    for i, t in enumerate(toks):
        if t.type == "inline" and t.content.startswith("Bash ") or (t.type == "inline" and t.content.startswith("**Bash**")):
            nxt = [x for x in toks[i + 1:] if x.type not in ("paragraph_close",)][:1]
            out.append((t.content, nxt[0].content.rstrip("\n") if nxt and nxt[0].type == "fence" else None))
    return out


def pairing() -> None:
    cmds = {1: "echo one", 2: "echo two", 3: "echo three"}
    started = [cmd_item(i, c, "item.started") for i, c in cmds.items()]
    completed = [cmd_item(2, cmds[2], "item.completed", "OUT-two\n"), cmd_item(3, cmds[3], "item.completed", "OUT-three\n"),
                 cmd_item(1, cmds[1], "item.completed", "OUT-one\n")]
    md = jq("codex.jq", "md", started + completed)
    check("pairing: nothing is printed when a command starts", jq("codex.jq", "md", started) == "", jq("codex.jq", "md", started))
    blocks = label_blocks(md)
    check("pairing: three label+result blocks in completion order",
          blocks == [("**Bash** `echo two`", "OUT-two"), ("**Bash** `echo three`", "OUT-three"), ("**Bash** `echo one`", "OUT-one")], str(blocks))
    check("pairing: each result sits directly under its own label (label, blank, fence, nothing between)",
          re.findall(r"\*\*Bash\*\* `([^`]+)`\n\n```\n(OUT-\w+)\n```", md) == [("echo two", "OUT-two"), ("echo three", "OUT-three"), ("echo one", "OUT-one")], md)
    check("pairing: no label without a result and no result without a label", md.count("**Bash**") == 3 and len(fences(md)) == 3, md)
    text = jq("codex.jq", "view", started + completed)
    check("pairing: text mode unchanged (labels at start, results at completion)",
          text == "> echo one\n> echo two\n> echo three\n  = exit 0: OUT-two \n  = exit 0: OUT-three \n  = exit 0: OUT-one \n", text)
    unmatched = jq("codex.jq", "md", [cmd_item(9, "sleep 99", "item.started")])
    check("pairing: an unmatched start prints nothing (append-only stream cannot supersede)", unmatched == "", unmatched)
    err = jq("codex.jq", "md", [cmd_item(1, "false", "item.completed", "bad", 2)])
    check("pairing: a failed command keeps label, error quote, fence together",
          unfold(err).startswith("**Bash** `false`\n\n> **error** exit 2\n\n```\nbad\n```"), err)


def stream_pairing() -> None:
    def call(id_: str, cmd: str) -> dict:
        return {"type": "tool_use", "id": id_, "name": "Bash", "input": {"command": cmd}}

    def res(id_: str, text: str) -> dict:
        return {"type": "tool_result", "tool_use_id": id_, "content": text, "is_error": False}
    asst = json.dumps({"type": "assistant", "message": {"content": [call("a", "echo A"), call("b", "echo B"), call("c", "echo C")]}})
    users = [json.dumps({"type": "user", "message": {"content": [res(i, "R-" + i)]}}) for i in ("b", "a")]
    md = jq("stream.jq", "md", [asst, *users])
    check("stream pairing: out-of-order results land under their own call, in arrival order",
          re.findall(r"\*\*Bash\*\* `echo (\w)`\n\n```\n(R-\w)\n```", md)[:2] == [("B", "R-b"), ("A", "R-a")], md)
    check("stream pairing: unmatched call at the end is a label with (no result)",
          md.rstrip("\n").endswith("**Bash** `echo C`\n\n*(no result)*"), md)
    check("stream pairing: each call is printed exactly once", md.count("**Bash**") == 3, md)
    live = jq("stream.jq", "mdlive", [asst, *users])
    check("stream pairing: mdlive holds the unanswered call back and is a prefix of md", "echo C" not in live and md.startswith(live), live)
    one = jq("stream.jq", "mdlive", [asst, users[0]])
    check("stream pairing: output for fewer lines is a prefix of output for more (append-only follower)", live.startswith(one) and len(one) < len(live), one)
    text = jq("stream.jq", "view", [asst, *users])
    check("stream pairing: text mode unchanged", text == "> Bash: echo A\n> Bash: echo B\n> Bash: echo C\n  = R-b\n  = R-a\n", text)


def noise() -> None:
    rm = '2026-10-18T06:18:13Z ERROR rmcp::transport::worker: worker quit with fatal: Transport channel closed'
    md = md_codex(["Reading additional input from stdin...", rm, rm, "stray stderr line", rm])
    check("noise: the stdin line is dropped", "Reading additional" not in md, md)
    check("noise: MCP warning is one italic note, even though it appeared three times",
          md.count("*MCP server unreachable, see raw.log*") == 1 and md.count("MCP") == 1, md)
    check("noise: the note is a paragraph, not a fence", "fence" in kinds(md) and [f[1] for f in fences(md)] == ["stray stderr line\n"], md)
    check("noise: the note renders as emphasis", "em_open" in [c.type for t in MarkdownIt("gfm-like").parse(md) if t.children for c in t.children], md)
    check("noise: other stderr stays in a text fence", "```text\nstray stderr line\n```" in md, md)
    check("noise: no rmcp detail leaks into Markdown", "rmcp" not in md and "Transport" not in md, md)
    text = subprocess.run(["awk", "-f", str(V / "clean.awk")], input=f"Reading additional input from stdin...\n{rm}\n{rm}\n", capture_output=True, text=True).stdout
    check("noise: text mode (clean.awk) unchanged", text == "Reading additional input from stdin...\n! an MCP server was unreachable (rmcp transport closed); raw.log has the detail\n", text)


def label_of(command: str) -> str:
    md = jq("codex.jq", "md", [cmd_item(1, command, "item.completed", "o")])
    return unfold(md).split("\n\n", 1)[0]


def inline_code(label: str) -> str | None:
    """The text of the inline code span in a label, according to markdown-it."""
    for t in MarkdownIt("gfm-like").parse(label):
        for c in t.children or []:
            if c.type == "code_inline":
                return c.content
    return None


def wrappers() -> None:
    cases = [
        ("double-quoted, inner quotes", '/bin/zsh -lc "node -e \'const p = require(\\"./package.json\\"); console.log(p.name)\'"',
         "node -e 'const p = require(\"./package.json\"); console.log(p.name)'"),
        ("single-quoted", "/bin/zsh -lc 'head -n 1 AGENTS.md'", "head -n 1 AGENTS.md"),
        ("quote-splice for an inner single quote", '/bin/zsh -lc "rg --files test/ | awk -F/ \'"\'$NF ~ /dist/ { n++ } END { print n+0 }\'"\'"',
         "rg --files test/ | awk -F/ '$NF ~ /dist/ { n++ } END { print n+0 }'"),
        ("bash -lc", "/bin/bash -lc 'ls -la'", "ls -la"), ("bare bash -lc", "bash -lc 'ls'", "ls"), ("sh -c", 'sh -c "echo hi"', "echo hi"),
        ("escaped dollar and backtick in double quotes", '/bin/zsh -lc "echo \\$HOME \\`date\\`"', "echo $HOME `date`"),
        ("no wrapper", "ls -la src", "ls -la src"),
        ("a command that merely mentions bash", "bash script.sh arg", "bash script.sh arg"),
    ]
    for name, raw, want in cases:
        got = inline_code(label_of(raw))
        check(f"wrapper: {name}", got == want, f"{raw!r} -> {got!r}, want {want!r}")
    for name, raw in (("unescaped $ inside double quotes (the outer shell expands it)", '/bin/zsh -lc "echo $HOME"'),
                      ("unterminated quote", "/bin/zsh -lc 'echo hi"), ("two words after the wrapper", "/bin/zsh -lc 'ls' extra"),
                      ("unquoted metacharacter", "/bin/zsh -lc ls;rm")):
        got = inline_code(label_of(raw))
        check(f"wrapper: unsafe case keeps the original text: {name}", got == raw, f"{got!r}")
    hostile_cmd = "/bin/zsh -lc \"printf '%s' \\`x\\` '\\$y' \\`\\`\\` ~~~ # h | <b>\""
    label = label_of(hostile_cmd)
    check("wrapper: hostile content stays one inline code span", inline_code(label) == "printf '%s' `x` '$y' ``` ~~~ # h | <b>" and kinds(label) == ["paragraph_open", "inline", "paragraph_close"], label)
    md = jq("codex.jq", "md", [cmd_item(1, hostile_cmd, "item.completed", "```\n`````\n# h")])
    check("wrapper: hostile command does not weaken the result fence", len(fences(md)) == 1 and len(fences(md)[0][0]) >= 6 and "heading_open" not in kinds(md), md)
    long = label_of("/bin/zsh -lc '" + "a" * 2500 + "'")
    code = inline_code(long)
    check("wrapper: a very long command is cut with an ellipsis", code is not None and code.endswith("...") and len(code) == 2003, str(len(code or "")))
    check("wrapper: text mode keeps the wrapper byte for byte",
          jq("codex.jq", "view", [cmd_item(1, "/bin/zsh -lc 'ls'", "item.started")]) == "> /bin/zsh -lc 'ls'\n")


def fixtures() -> None:
    f = V.parent / "test/fixtures"
    r1 = md_codex((f / "run1.raw.log").read_text().splitlines())
    pairs = re.findall(r"\*\*Bash\*\* `([^`]+)`\n\n```\n(.*?)\n```", r1, re.S)
    check("fixture run1: three commands, each with its own result, unwrapped",
          [p[0][:12] for p in pairs] == ["head -n 1 AG", "rg --files -", "node -e 'con"] and [p[1][:3] for p in pairs] == ["# i", "1", "{\n "], str(pairs))
    check("fixture run1: no wrapper text and no stdin line left", "/bin/zsh" not in r1 and "Reading additional" not in r1, r1)
    check("fixture run1: one MCP note, not fenced", r1.count("*MCP server unreachable") == 1 and "rmcp" not in r1, r1)
    r2 = md_codex((f / "run2.raw.log").read_text().splitlines())
    check("fixture run2 (resume): MCP note once, answer shown", r2.count("*MCP server unreachable") == 1 and "shortest value" in r2, r2)


XCRUN = [
    "git: warning: confstr() failed with code 5: couldn't get path of DARWIN_USER_TEMP_DIR; using /tmp instead",
    "git: error: couldn't create cache file '/tmp/xcrun_db-uzPWrjBW' (errno=Operation not permitted)",
    "2026-10-18 02:28:32.124 xcodebuild[42628:9741937]  DVTFilePathFSEvents: Failed to start fs event stream.",
    '2026-10-18 02:28:32.371 xcodebuild[42628:9741936] [MT] DVTDeveloperPaths: Failed to get length of DARWIN_USER_CACHE_DIR from confstr(3), error = Error Domain=NSPOSIXErrorDomain Code=5 "Input/output error". Using NSCachesDirectory instead.',
]
NOTE = "*macOS xcrun cache errors hidden (sandbox), see raw.log*"


def result_md(output: str, code: int = 0) -> str:
    return jq("codex.jq", "md", [cmd_item(1, "git rev-parse HEAD", "item.completed", output, code)])


def xcrun() -> None:
    real = "\n".join(XCRUN + XCRUN) + "\ne26d6a1\n"
    md = result_md(real)
    check("xcrun: the real lines leave the fence, the real output stays", [f[1] for f in fences(md)] == ["e26d6a1\n"], md)
    check("xcrun: one note per result, after the fence", md.count(NOTE) == 1 and md.rindex(NOTE) > md.rindex("```"), md)
    check("xcrun: nothing of the noise is left anywhere", not any(w in md for w in ("confstr", "xcrun_db", "DVT", "xcodebuild")), md)
    check("xcrun: the note is a paragraph of emphasis", "em_open" in [c.type for t in MarkdownIt("gfm-like").parse(md) if t.children for c in t.children], md)
    both = result_md(real) + result_md(real)
    check("xcrun: two results with noise get one note each", both.count(NOTE) == 2, both)
    only = result_md("\n".join(XCRUN) + "\n")
    check("xcrun: a result of only noise shows the note and an empty marker", [f[1] for f in fences(only)] == ["(no output)\n"] and only.count(NOTE) == 1, only)
    plain_empty = result_md("")
    check("xcrun: a genuinely empty result has no note and no marker", NOTE not in plain_empty and "(no output)" not in plain_empty, plain_empty)
    lookalikes = ["confstr", "grep xcrun file", "xcode and /tmp", "/tmp/xcrun_db-1: No such file", "git: warning: something else",
                  "note: " + XCRUN[0], XCRUN[1] + " extra", "xcodebuild: error: not that one"]
    legit = result_md("\n".join(lookalikes) + "\n")
    check("xcrun: lines that merely mention xcode, tmp or confstr are kept", [f[1] for f in fences(legit)] == ["\n".join(lookalikes) + "\n"] and NOTE not in legit, legit)
    mixed = result_md("confstr\n" + XCRUN[0] + "\ngrep xcrun file\n")
    check("xcrun: legitimate lines beside noise stay in order", [f[1] for f in fences(mixed)] == ["confstr\ngrep xcrun file\n"], mixed)
    err = result_md(XCRUN[0] + "\nfatal: bad\n", 128)
    check("xcrun: a failing result keeps its error quote and real stderr", "> **error** exit 128" in err and "fatal: bad" in err and NOTE in err, err)
    sm = jq("stream.jq", "md", [json.dumps({"type": "user", "message": {"content": [{"type": "tool_result", "content": XCRUN[0] + "\nok", "is_error": False}]}})])
    check("xcrun: ccx results go through the same rule", NOTE in sm and "confstr" not in sm, sm)
    txt = jq("codex.jq", "view", [cmd_item(1, "git x", "item.completed", XCRUN[0] + "\nok")])
    check("xcrun: text mode (clean.log) is byte-for-byte what it was", "  = exit 0: " + XCRUN[0] + " ok" in txt, txt)
    fx = md_codex((V.parent / "test/fixtures/run3.raw.log").read_text().splitlines())
    check("fixture run3 (real xcrun noise): two clean results, one note on the git result", fx.count(NOTE) == 1 and "xcrun_db" not in fx and "DVT" not in fx and "e26d6a1" in fx, fx)


def rest_md(lines: list[str], last: str | None = None, seen: Path | None = None) -> str:
    """The rest of a chained command as clean.md gets it (mdrest in hooks/mdpipe.ts)."""
    return pipe_rest(lines, last, seen, "mdraw", ["--unbuffered"])


def rest_text(lines: list[str], last: str | None = None, seen: Path | None = None) -> str:
    """The same stream as the text the Bash call returns (restfilter in hooks/mdpipe.ts)."""
    return pipe_rest(lines, last, seen, "rest", [])


def pipe_rest(lines: list[str], last: str | None, seen: Path | None, mode: str, jqflags: list[str]) -> str:
    import tempfile
    tmp = Path(tempfile.mkdtemp())
    lastf = tmp / "last.md"
    if last is not None:
        lastf.write_text(last)
    a = subprocess.run(["awk", "-v", f"seen={seen or ''}", "-f", str(V / "clean.awk")], input="\n".join(lines) + "\n", capture_output=True, text=True, check=True)
    b = subprocess.run(["awk", "-v", f"last={lastf if last is not None else ''}", "-f", str(V / "rest.awk")], input=a.stdout, capture_output=True, text=True, check=True)
    return subprocess.run(["jq", *jqflags, "-R", "-r", "--arg", "mode", mode, "-f", str(V / "codex.jq")], input=b.stdout, capture_output=True, text=True, check=True).stdout


def chain_out() -> list[str]:
    return (V.parent / "test/fixtures/run3.chain.out").read_text().splitlines()


def rest_noise() -> None:
    import tempfile
    lines = chain_out()
    md = rest_md(lines, (V.parent / "test/fixtures/run3.last.md").read_text())
    check("rest md: no stdin line, no raw rmcp line, no hostname", "Reading additional" not in md and "rmcp" not in md and "example.invalid" not in md, md)
    check("rest md: three raw MCP errors become one note", md.count("*MCP server unreachable, see raw.log*") == 1, md)
    check("rest md: the answer already shown is not repeated", "nothing modified" not in md and fences(md) == [], md)
    kept = rest_md(lines)
    check("rest md: without an answer file the answer line is still shown (nothing else is guessed)", [f[1] for f in fences(kept)] == [lines[-1] + "\n"], kept)
    txt = rest_text(lines, (V.parent / "test/fixtures/run3.last.md").read_text())
    check("rest text: only the one MCP line is left, nothing of the answer", txt == "! an MCP server was unreachable (rmcp transport closed); raw.log has the detail\n", repr(txt))
    check("rest text: the same stream without the answer file keeps the answer", txt in rest_text(lines) and rest_text(lines).endswith(lines[-1] + "\n"))
    d = Path(tempfile.mkdtemp()) / "seen"
    first, second = rest_md(lines, None, d), rest_md(lines, None, d)
    check("rest md: the MCP note is made once per seen-file, across processes", first.count("MCP server unreachable") == 1 and "MCP server" not in second, first + "|" + second)
    check("rest md: without a seen-file each process makes its own (clean.awk unchanged)", rest_md(lines).count("MCP server unreachable") == 1 and rest_md(lines).count("MCP server unreachable") == 1)
    d2 = Path(tempfile.mkdtemp()) / "seen2"
    t1, t2 = rest_text(lines, None, d2), rest_text(lines, None, d2)
    check("rest text: one MCP line across the out and err passes", "MCP" in t1 and "MCP" not in t2, t1 + "|" + t2)


def answer_dedupe() -> None:
    ans = "line one\nline two\n"
    cases = {
        "the whole block is dropped": (["x", "line one", "line two", "y"], ["x", "y"]),
        "a partial block is kept": (["line one", "other", "line two"], ["line one", "other", "line two"]),
        "a split block (not contiguous) is kept": (["line one", "x", "line two"], ["line one", "x", "line two"]),
        "a block cut by the end of the stream is kept": (["a", "line one"], ["a", "line one"]),
        "a restart inside a partial match still matches": (["line one", "line one", "line two"], ["line one"]),
        "two copies are both dropped": (["line one", "line two", "z", "line one", "line two"], ["z"]),
        "a single line that is one of the answer lines is kept": (["line two"], ["line two"]),
    }
    for name, (src, want) in cases.items():
        out = subprocess.run(["awk", "-v", "last=" + write_tmp(ans), "-f", str(V / "rest.awk")], input="\n".join(src) + "\n", capture_output=True, text=True, check=True).stdout.splitlines()
        check(f"answer dedupe: {name}", out == want, str(out))
    nonl = subprocess.run(["awk", "-v", "last=" + write_tmp("only line"), "-f", str(V / "rest.awk")], input="only line\nkept\n", capture_output=True, text=True, check=True).stdout
    check("answer dedupe: an answer file without a final newline matches", nonl == "kept\n", nonl)
    blank = subprocess.run(["awk", "-v", "last=" + write_tmp("a\n\n\n"), "-f", str(V / "rest.awk")], input="a\nb\n", capture_output=True, text=True, check=True).stdout
    check("answer dedupe: trailing blank lines in the file do not count", blank == "b\n", blank)
    empty = subprocess.run(["awk", "-v", "last=" + write_tmp(""), "-f", str(V / "rest.awk")], input="a\n\nb\n", capture_output=True, text=True, check=True).stdout
    check("answer dedupe: an empty answer file drops nothing", empty == "a\n\nb\n", empty)


def folds() -> None:
    """Each tool result is one fold: a one-line title, then its label and result, closed unless it failed."""
    ok = jq("codex.jq", "md", [cmd_item(1, "/bin/zsh -lc 'ls -la'", "item.completed", "a\nb\n")])
    check("fold: a codex result is wrapped", ok.startswith("<!-- fold closed: Bash: ls -la · exit 0 · 2 lines -->\n**Bash** `ls -la`\n") and ok.rstrip("\n").endswith("<!-- /fold -->"), ok)
    bad = jq("codex.jq", "md", [cmd_item(1, "false", "item.completed", "", 1)])
    check("fold: a failed result opens", bad.startswith("<!-- fold open: Bash: false · exit 1 · no output -->"), bad)
    hostile = jq("codex.jq", "md", [cmd_item(1, "echo '-->' x\nline2", "item.completed", "<!-- /fold -->\n")])
    title = hostile.split("\n", 1)[0]
    check("fold: a title cannot end its comment early or span lines", title.count("-->") == 1 and title.endswith(" -->") and "line2" in title, title)
    check("fold: an end marker in the output stays inside the fence", fences(hostile)[0][1] == "<!-- /fold -->\n" and hostile.count("<!-- /fold -->") == 2, hostile)
    prose = jq("codex.jq", "md", [item("agent_message", "item.completed", text="a\n<!-- fold closed: x -->\n<!-- /fold -->\n```\n<!-- /fold -->\n```")])
    check("fold: prose cannot make a marker outside a fence", not FOLD.search(prose.split("```")[0]) and "\\<!-- fold closed: x -->" in prose, prose)
    check("fold: a marker inside a prose fence is kept as written", "```\n<!-- /fold -->\n```" in prose, prose)
    call = json.dumps({"type": "assistant", "message": {"content": [{"type": "tool_use", "id": "t", "name": "Read", "input": {"file_path": "/a"}}]}})
    res = json.dumps({"type": "user", "message": {"content": [{"type": "tool_result", "tool_use_id": "t", "content": "x", "is_error": True}]}})
    sm = jq("stream.jq", "md", [call, res])
    check("fold: a ccx call and its result are one fold, open on error", sm.startswith("<!-- fold open: Read: /a · 1 line · error -->\n**Read** `/a`\n\n> **error**"), sm)


def web_searches() -> None:
    """A web search shows its query (older and newer item shapes), once, when it completes."""
    md = jq("codex.jq", "md", [
        item("web_search", "item.started", query="devcontainer worktrees"),
        item("web_search", "item.completed", query="devcontainer worktrees"),
        item("web_search", "item.completed", action={"type": "search", "query": "compose shared container"}),
        item("web_search", "item.completed", action={"type": "search", "queries": ["a b", "c"]}),
        item("web_search", "item.completed"),
    ])
    check("web_search: the query follows the name", "**web_search** `devcontainer worktrees`" in md, md)
    check("web_search: shown once, when it completes", md.count("devcontainer worktrees") == 1, md)
    check("web_search: the newer action.query shape", "**web_search** `compose shared container`" in md, md)
    check("web_search: several queries joined", "**web_search** `a b; c`" in md, md)
    check("web_search: no query still names the search", md.rstrip().endswith("**web_search**"), md)


def file_changes() -> None:
    """A file change names its files and kinds, once, when it completes."""
    md = jq("codex.jq", "md", [
        item("file_change", "item.started", changes=[{"path": "compose.yaml", "kind": "update"}]),
        item("file_change", "item.completed", changes=[{"path": "compose.yaml", "kind": "update"}, {"path": "dev/new.sh", "kind": "add"}]),
        item("file_change", "item.completed"),
    ])
    check("file_change: kinds and paths follow the name", "**file_change** `update compose.yaml, add dev/new.sh`" in md, md)
    check("file_change: shown once, when it completes", md.count("compose.yaml") == 1, md)
    check("file_change: no changes still names it", md.rstrip().endswith("**file_change**"), md)


def long_titles() -> None:
    """A long command keeps 240 characters in its fold title (the pane wraps it) and its whole label."""
    cmd = "echo " + "x" * 400
    md = jq("codex.jq", "md", [item("command_execution", "item.completed", command=cmd, exit_code=0, aggregated_output="ok")])
    title = md.split("\n", 1)[0]
    check("long title: 240 characters of the command, then ...", "x" * 230 in title and "x" * 300 not in title and "..." in title, title)
    check("long title: the label inside holds the whole command", "x" * 400 in md, md[:120])


def write_tmp(text: str) -> str:
    import tempfile
    p = Path(tempfile.mkdtemp()) / "last.md"
    p.write_text(text)
    return str(p)


def static() -> None:
    """File-level checks the hook test sandbox cannot do (it may not import node:fs)."""
    root = V.parent
    req = re.search(r"TEXTUAL_REQ = '([^']+)'", (root / "hooks/panescript.ts").read_text()).group(1)
    check("static: viewer header repeats TEXTUAL_REQ", f'dependencies = ["{req}"]' in (V / "codex_view.py").read_text())
    check("static: version is 0.9.0", json.loads((root / ".claude-plugin/plugin.json").read_text())["version"] == "0.9.0")
    for f in ("codex.jq", "stream.jq"):
        jq_src = (V / f).read_text()
        check(f"static: {f} has one events parser feeding txt and md",
              all(x in jq_src for x in ("def events:", "def txt:", "def md:", 'include "md"')) and jq_src.count("def events:") == 1)
    for f in sorted(root.glob("**/*.py")) + sorted(V.glob("*.jq")) + sorted((root / "hooks").glob("*.ts")) + sorted(root.glob("test/*.*")):
        n = len(f.read_text().splitlines())
        check(f"static: {f.name} under 500 lines ({n})", n < 500)


if __name__ == "__main__":
    for fn in (static, hostile, tilde_and_ticks, normal, truncated, errors, passthrough_and_prose, partial, stream_normal, pairing, stream_pairing, noise, wrappers, fixtures, xcrun, rest_noise, answer_dedupe, folds, web_searches, file_changes, long_titles):
        fn()
    print(f"\n{len(FAILS)} failed" if FAILS else "\nall md cases passed")
    sys.exit(1 if FAILS else 0)
