# Fixture — bad flag that should be flagged

The skill-flag validator must fail on the invocation below, because
`wipnote feature show` does not register `--this-flag-doesnt-exist`.

```
wipnote feature show feat-abc123 --this-flag-doesnt-exist
```
