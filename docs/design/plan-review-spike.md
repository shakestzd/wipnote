# Plan review spike: what a Claude Artifact can and cannot do

`plan-review-spike.html` is the throwaway page used to test the artifact behaviour the
visual-plan review surface depends on (see `visual-plan-target.md`). It is a fragment, not a
standalone page: publish it as an artifact declaring `{db:{}, user:{}, comments:{}}`
(runtime contract 0.2.70). Nothing in it is a real plan.

## Findings

| Question | Result | How it was checked |
|---|---|---|
| Does a page write to the database wake the session? | **No.** | Two decisions saved from the page produced zero notifications. |
| Does a comment sent to Claude wake the session? | **Yes**, in about 20 seconds. | One comment produced one notification. |
| Who can send to Claude from a page? | **Editors of the artifact only**, and only while a session is watching it. | `canSendToClaude()` reports `writers_only` or `no_session` otherwise. |
| Can the session read what the page saved? | **Yes**, with the hash it stored. | Read through the artifact database tool after the comment woke the session. |
| Do page writes check versions? | **No.** Page writes are last-writer-wins. Only writes from the session need `if_version`. | `set` from the page never rejected. |
| Do other viewers see updates live? | **Not tested.** | Needs a second viewer to confirm without a reload. |
| Does the clipboard work inside an artifact? | **Not tested.** | |
| Can reviewers be shown by name? | **No.** Reviewer ids are opaque (`u_…`). | `profiles` gave no names. |

## What this means for the design

- The comment is the doorbell and the database is the data. The reviewer's explicit
  "Send to Claude" wakes the session; the session then reads structured decisions from the
  database. Do not rely on database writes to notify anyone.
- Only editors can ring the doorbell. A team with view-only reviewers needs another way to
  tell the session (or a person) that a review is done.
- The session can detect stale approvals itself: the page stores a hash of the
  whitespace-normalised slice text (FNV-1a, 32-bit), and Python reproduces it exactly
  (`8df6eacc`, `2dab195f` for the two spike slices).
- Because page writes overwrite each other, a "changed elsewhere" banner with a
  "Show latest" action is the honest behaviour, not a version conflict.

## Cleanup

The spike artifact and its `reviews/plan-spike/slices/*` documents are disposable and
should be deleted once the untested rows above are either confirmed or dropped.
