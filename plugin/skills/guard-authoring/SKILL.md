---
name: guard-authoring
description: "Authors, approves, and maintains project guard profiles (.wipnote/guard-profile.yaml). Use when setting custom quality, completion, or yolo gates, scoping guards per directory in a monorepo, or re-approving a profile after it drifts."
disable-model-invocation: true
---

# Guard Authoring Skill

## What a guard profile is

`.wipnote/guard-profile.yaml` is a hand-maintained YAML file committed to the repository.
It replaces wipnote's manifest autodetection with an explicit, version-controlled list of
commands. Full reference:
[docs/guard-profiles.md](https://github.com/shakestzd/wipnote/blob/main/docs/guard-profiles.md).

Copy this checklist for every profile change:

```
Guard profile change:
- [ ] 1. Profile edited (schema below)
- [ ] 2. Re-approved with `wipnote guard init`
- [ ] 3. `wipnote check --gate` output names the profile's guards
- [ ] 4. Gate clean (see "Verify after every edit")
```

---

## Schema

```yaml
guards:
  quality:          # run by `wipnote check --gate`
    - name: go-build               # short identifier (required)
      cmd: go build ./...          # shell command (required, non-empty)
      cwd: backend                 # repo-root-relative working dir (optional)
      applies_when:                # narrow when guard is active (optional)
        paths: ["backend/**/*.go"] # ** matches zero or more path segments
  completion:       # work-item completion gate
    - name: go-test
      cmd: go test ./...
  yolo:             # per-commit gate in yolo sessions
    - name: go-vet
      cmd: go vet ./...
approved:
  signature: "sha256:..."   # content hash; written by wipnote, never by hand
  by: "Alice"
  at: "2026-01-15T10:30:00Z"
```

Fields: `name` (required), `cmd` (required, non-empty), `cwd` (optional, repo-root-relative),
`applies_when.paths` (optional forward-slash globs — `**` crosses `/`, `*` does not).
Do not hand-edit `approved.signature`.

---

## Polyglot / monorepo example

Only the `guards.quality` delta is shown; the other phases follow the same shape.

```yaml
guards:
  quality:
    - name: go-test
      cmd: go test ./...
      cwd: backend
      applies_when:
        paths: ["backend/**/*.go"]
    - name: npm-test
      cmd: npm test
      cwd: frontend
      applies_when:
        paths: ["frontend/**"]
    - name: py-test
      cmd: uv run pytest
      cwd: dataservice
      applies_when:
        paths: ["dataservice/**/*.py"]
```

`cwd` routes each toolchain to its subdirectory. `applies_when.paths` gates a guard on
whether matching files EXIST in the repository — not on which files a commit changed
(wipnote does not inspect the changed-file set). A guard whose globs match no repo file is
skipped; one that matches any repo file runs on every gate. Use it to scope guards to a
project layout, not to skip guards per-commit.

---

## Approval model and trust boundary

wipnote only honors a profile when `approved.signature` matches the sha256 of the guard
content. Falls back to autodetection (no error) when: profile absent, signature empty,
signature mismatch (edited after approval), or validation failure (unknown phase key or
empty `cmd`). The signature is order-independent — reformatting YAML does not break it,
but changing any guard field does.

---

## Launch-time setup and `wipnote guard init`

Every interactive launch (`wipnote claude/yolo/dev/codex/antigravity`) auto-runs setup:
if no approved profile exists and the launch is interactive, it proposes guards from
manifests, prompts `[y/N]`, and on `y` signs + commits the profile. Non-interactive
launches skip silently.

```bash
wipnote guard init    # explicit: propose, review, approve, commit (re-runnable)
```

## Verify after every edit

1. Run `wipnote guard init` to re-approve and record a fresh signature.
2. Run `wipnote check --gate`.
3. Confirm the output names your profile's guards (their `name` values), not autodetected gates such as `go build` / `go vet` / `go test`. Autodetected gates mean the profile was not honored; see the fallback cases above.
4. Fix the profile and repeat until the gate is clean and runs your guards.

---

## Safe default

No approved profile → autodetection from `go.mod`, `package.json`, `Makefile`, etc.,
with a hint to run `wipnote guard init`. Use a committed profile for custom commands,
subdirectory routing, or `applies_when` filtering.
