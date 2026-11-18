#!/usr/bin/env python3
"""Helpers for scripts/prompt-ab.sh (feat-217fb5f2).

behaviours: read one worker transcript and print which checked behaviours held.
compare:    read per-run behaviour files for two refs and write the comparison.

The pass rule lives in is_regression(). Its source is docs/design/prompt-ab.md.
"""
import argparse
import glob
import json
import os
import re
import sys

BEHAVIOURS = ["started", "test_first", "gate", "commit_names_item"]


def tool_calls(path):
    """Yield (tool name, input dict) in transcript order."""
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            try:
                rec = json.loads(line)
            except ValueError:
                continue
            content = (rec.get("message") or {}).get("content")
            if rec.get("type") != "assistant" or not isinstance(content, list):
                continue
            for part in content:
                if isinstance(part, dict) and part.get("type") == "tool_use":
                    yield part.get("name", ""), part.get("input") or {}


def is_test_file(p):
    return p.endswith("_test.go")


GO_TARGET = re.compile(r"(?:>>?|\btee(?:\s+-a)?)\s*(\S+\.go)\b")


def go_writes(name, inp):
    """Return the .go paths a tool call writes: Write/Edit input or a shell redirect."""
    if name in ("Write", "Edit", "MultiEdit"):
        p = inp.get("file_path", "")
        return [p] if p.endswith(".go") else []
    if name == "Bash":
        return GO_TARGET.findall(inp.get("command", ""))
    return []


def behaviours(path, item):
    seen = {"build": False, "vet": False, "test": False, "gate_cmd": False, "start": False, "commit": False}
    test_edit = False
    first_impl_ok = None  # decided at the first non-test .go write
    for name, inp in tool_calls(path):
        for p in go_writes(name, inp):
            if is_test_file(p):
                test_edit = True
            elif first_impl_ok is None:
                first_impl_ok = test_edit
        if name != "Bash":
            continue
        cmd = inp.get("command", "")
        if re.search(r"wipnote\s+(feature|bug|spike)\s+start\s+" + re.escape(item), cmd):
            seen["start"] = True
        for k in ("build", "vet", "test"):
            seen[k] |= bool(re.search(r"\bgo\s+" + k + r"\b", cmd))
        seen["gate_cmd"] |= bool(re.search(r"\bwipnote\s+check\b.*--gate", cmd))
        if re.search(r"\bgit\s+commit\b", cmd) and item in cmd:
            seen["commit"] = True
    return {
        "started": seen["start"],
        "test_first": bool(first_impl_ok),
        "gate": seen["gate_cmd"] or (seen["build"] and seen["vet"] and seen["test"]),
        "commit_names_item": seen["commit"],
    }


def is_regression(old_passes, new_passes, runs):
    """Held in all runs on the old ref and in fewer than two on the new ref."""
    return old_passes == runs and new_passes < 2


def load_side(results, side):
    """Return {task: [run dict, ...]} for one side."""
    out = {}
    for task_dir in sorted(glob.glob(os.path.join(results, side, "*"))):
        runs = []
        for f in sorted(glob.glob(os.path.join(task_dir, "run*.json"))):
            with open(f, encoding="utf-8") as fh:
                runs.append(json.load(fh))
        out[os.path.basename(task_dir)] = runs
    return out


def compare(args):
    old, new = load_side(args.results, "old"), load_side(args.results, "new")
    lines = ["# Prompt before-and-after comparison", "",
             f"Old ref: `{args.old_ref}`. New ref: `{args.new_ref}`. Runs per task per ref: {args.runs}.", "",
             "| task | behaviour | old | new | verdict |", "|---|---|---|---|---|"]
    flagged = 0
    for task in sorted(set(old) | set(new)):
        for b in BEHAVIOURS:
            o = sum(1 for r in old.get(task, []) if r.get(b))
            n = sum(1 for r in new.get(task, []) if r.get(b))
            bad = is_regression(o, n, args.runs)
            flagged += bad
            lines.append(f"| {task} | {b} | {o}/{args.runs} | {n}/{args.runs} | {'REGRESSION' if bad else 'ok'} |")
    lines += ["", f"Regressions flagged: {flagged}", ""]
    with open(args.out, "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))
    print("\n".join(lines))
    return 1 if flagged else 0


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    b = sub.add_parser("behaviours")
    b.add_argument("--item", required=True)
    b.add_argument("transcript")
    c = sub.add_parser("compare")
    for flag in ("--results", "--old-ref", "--new-ref", "--out"):
        c.add_argument(flag, required=True)
    c.add_argument("--runs", type=int, default=3)
    args = ap.parse_args()
    if args.cmd == "behaviours":
        print(json.dumps(behaviours(args.transcript, args.item)))
        return 0
    return compare(args)


if __name__ == "__main__":
    sys.exit(main())
