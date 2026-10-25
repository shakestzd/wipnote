# Provenance of the orch reference

Reference material for plan-28b4a7f6 slice 1 (feat-0031fd91). This folder holds a scrubbed copy of orch's README
and this note. It holds no orch code, and nothing here is built, run or imported by wipnote. The port checklist is
`docs/design/orch-inventory.md`.

## What was read

An archive the owner uploaded on 2026-10-25 (an orch handoff bundle), unpacked outside the repository and read
only: nothing in it was built, run, imported or installed. The orch source stays outside the repository
(q-orch-source: README and inventory only).

## Version

- **orch 0.1.0.** `herdr-research/orch/pyproject.toml`: project `orch`, version `0.1.0`, Python >= 3.12, runtime
  dependencies pydantic, filelock, typer.
- **No git metadata.** The archive has no `.git` folder, packed refs or `HEAD` file anywhere, so no commit can be
  named. The read is identified by version strings and the SHA-256 hashes below instead.
- **Hook binary.** `orch/bin/orch-hook.VERSION` = `0.1.0`; the `orch-hook --version` text recorded on the owner's
  Mac is `orch-hook 0.1.0 spec 1 darwin/arm64 go1.26.10` (from `orch-hook-go/mac-evidence`, not run here).
- **Hook event format.** The bundled binaries in `orch/bin/` write event format version 1 (with `seq`).
  `herdr-research/orch-hook-go/hook_spec.json` is spec_version 1, event_version 2 (no `seq`), and the archive's
  handoff text says v2. Both formats are recorded in the inventory for slice 18.
- **herdr 0.9.5.** The target herdr is 0.9.5 (`herdr-research/cli-help.txt` line 1: a herdr 0.9.5 CLI help dump of
  2026-10-23; `api-schema.json` socket protocol 22).
- **Siblings in the archive.** `orch-ts` 0.1.0 and `orch-conformance` 0.1.0 (`package.json`), `herdr-watch` 0.1.0.
- **Dates.** Newest files: `HANDOFF.md`, `orch/README.md` and `orch/MANUAL-TEST.md` (2026-10-25). The handoff text
  calls its state "verified on 2026-10-24, rounds 1 to 6 done".

## File hashes (SHA-256)

Paths relative to `herdr-research/`.

| File | SHA-256 |
|---|---|
| `orch/README.md` (original, before scrubbing) | `babac6d87d1f3c71c9a498480115dbc44da608e775a772f38b0d2f5bc99ef113` |
| `orch/pyproject.toml` | `932ce55878d58d4e25cc2ab65ce9d42c5e127ff2673f1fdb8a3b3b52c979e397` |
| `orch/uv.lock` | `3e208a6160efe18d3b235e980357a77227419d08d6e32a03c2f27e6ac7079e9d` |
| `orch/bin/orch-hook.VERSION` | `e9dd8507f4bf0c6f42458e41aea833ad0bd3f6127272335eee9bf4d58541ed67` |
| `orch/src/orch/cli.py` | `ca6bda8a38bff348b70615e01a0112fef2ed289b0dd01a769117da6758e0ce49` |
| `orch/src/orch/hookcore.py` | `e7386b63eec7a73526f8b0dee961c36dd9d67312b067f52f562c1950da669659` |
| `orch/src/orch/models/__init__.py` | `bb039492436ee4d3d9053a329a1f4634012c060abe69e1e62abc83bd26a25738` |
| `orch/src/orch/models/settings.py` | `5cb5af44065b0511eecae772d7b3c6afd1edc61d1f9bc478305c340c5bebdade` |
| `orch/src/orch/probes.py` | `917bcea6c4dc95aabb759750d87decad8f5c18c56e7ef881809910b42cbbc00d` |
| `orch/src/orch/stores/atomic.py` | `f0bd3ba87423446602470fd3dd34985a0786099d59e74195c0723d4faa920819` |
| `orch/src/orch/stores/mailbox.py` | `04c9e2317199038f3cbdc327ec4a4e6d0f3529734f415ea3565349d546307439` |
| `orch/src/orch/stores/registry.py` | `d5795b545af8cc34c8f9276e75523dd9b11797851149ec3acb8e0fc468058fb0` |
| `orch/src/orch/service/launch.py` | `e32c8446dd43fecd27d825ed37d65f8c1d5b49b80588ab9246d4619ee71eae37` |
| `orch/src/orch/service/messaging.py` | `64151f831dfe5c94b4fee9b4356dba7675cb74252aeb0076852e10507e36c833` |
| `orch/src/orch/service/placement.py` | `3f986676377d456f438165b82db13b64aee4bc898f0cf44f7411872a121937a9` |
| `orch/src/orch/service/questions.py` | `13791afd90d60c8ad2dd30f761198178552f3bc300da04b7eb54b3c9e588728b` |
| `orch/src/orch/service/readiness.py` | `c0d886b7c965a531d260ff72d570848a64436a57476453038a717cc5c9870960` |
| `orch/src/orch/service/sessions.py` | `82db71e9142db20376c1a73db28c59ab839a3590a4fff9ed55ff869cbe75f132` |
| `orch/src/orch/service/shutdown.py` | `af4a55e35bac67f8dc2f231894269b311f41fa32370b5454f535786c695b14b9` |
| `orch/src/orch/service/transports.py` | `35ca6ad1898104f55a45d9c2860060ff81003911dd92c324628dab364983040f` |
| `cli-help.txt` | `e765d24643c78e65807e9ada9e143b2a8a00f29adc8d1ad68b40333d86f0231e` |
| `api-schema.json` | `9e2af207e9aa8183d4aeca5fde9cc48e7909bb40cdbd7cf21608a6d3ea78075b` |

Tree hash of orch's source, hooks, binaries and scripts plus `pyproject.toml` and `README.md` (69 files), run in
`orch/`:

```
find src hooks bin scripts pyproject.toml README.md -type f | LC_ALL=C sort | xargs sha256sum | sha256sum
59945f81a9426d23a67048838a983364a977587cbcffdab1564ea680fc2984d8
```

## License

None found. No LICENSE, COPYING or NOTICE file, and no license or SPDX header in any source file.
`orch-ts/package.json` and `orch-conformance/package.json` say `"license": "UNLICENSED"`; `orch/pyproject.toml`
has no license field. Treat all of it as the owner's unlicensed private work: reference only, no code copied.

## Layout of the archive

```
_shared-orch-plugin/        Claude Code plugin "orch": one skill, delegate-to-a-session (docs only, no code)
herdr-research/
  HANDOFF.md                read-first summary, state, open work, an integration prompt for wipnote
  orch/                     the Python orch (reference): src/orch/, hooks/, bin/ (uv wrapper + prebuilt Go hooks),
                            scripts/, tests/ (about 50 files), README.md, MANUAL-TEST.md, REAL-HERDR-FINDINGS.md,
                            BUILD-DONE, FIX-DONE, FIX2-DONE, an embedded .wipnote/ (one feature, two claims)
  orch-hook-go/             Go rewrite of the hook program only; parity tests, benchmarks, prebuilt dist/, mac-evidence/
  orch-ts/                  TypeScript port of an older Python snapshot (parked), with that snapshot in reference/
  orch-conformance/         language-neutral corpus: 484 scenarios, a Node fake for herdr, ps and dev, a runner
  orch-parallel/            briefs and DONE notes of the build rounds
  tools/                    herdr-watch (read-only watcher) and a script for leaked helper servers
  bench-mount/              about 10,700 files of bind-mount benchmark output (per-event files), scripts
  report*.md, brief*.md     research notes (herdr overlap, layout, observability, storage, container messaging)
  cli-help.txt              herdr 0.9.5 --help dump
  herdr-skill.md            herdr's agent skill text
  api-schema.json           herdr socket API JSON schema
  orch-issues-report.md     problems met while building, fixed and open
  orch-review-brief.md, orch-review-codex.md   a Codex review of an early orch and its brief
```

## What was scrubbed

`README.md` here is `orch/README.md` with these changes and nothing else:

- An HTML comment at the top marks it as a scrubbed reference copy.
- An absolute home path (which contained the owner's login name) in the quick-start install line is replaced with
  `<path>`.
- The names of the owner's other projects (in the dependency notes) are replaced with `<project>`.

Its hash therefore differs from the original's above. Relative links in it (for example `../report-reinvention.md`)
point into the archive and do not resolve here. Home-relative orch defaults such as `~/.cache/orch/` and `~/work`
are kept, because they are documented settings, not host paths.

Categories withheld from all three files (values not reproduced anywhere in this repository):

- absolute host paths and home directories of the owner's machine;
- the owner's user and login names;
- names of the owner's employer and other projects;
- GitHub account names and issue URLs that carry them;
- email addresses, and test-fixture secrets from the redaction tests;
- machine names.
