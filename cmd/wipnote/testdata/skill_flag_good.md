# Fixture — only real flags

Each of the invocations below uses a flag that is registered on its target
command. The skill-flag validator must not report any violation.

```
wipnote track show trk-abc123 --format json
wipnote track show trk-abc123 --deep
wipnote feature show feat-abc123 --format json
```
