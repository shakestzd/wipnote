# Dev and yolo sessions in Codespaces

Moved from `CLAUDE.md` in slice 12 of plan-2622b3f7 (feat-6f99bc47).

Codespaces clients disconnect on idle, browser refresh or network blips. That kills long sessions. Wrap them in tmux with `--tmux`.

## Dev mode

```bash
wipnote claude --dev --tmux
```

The first run creates a tmux session named `wipnote-dev`. On disconnect, the session survives. Run the same command to reattach.

## Yolo mode

```bash
wipnote yolo --dev --tmux
```

The first run creates a tmux session named `wipnote-yolo`. Reattach the same way.

## Detach and kill

- Detach by hand with `Ctrl-b d`.
- Kill a session with `tmux kill-session -t wipnote-dev` or `tmux kill-session -t wipnote-yolo`.

`wipnote antigravity --tmux` works the same way with a session named `wipnote-antigravity`.
