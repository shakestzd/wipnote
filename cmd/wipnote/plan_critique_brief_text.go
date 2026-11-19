package main

// The critic brief text. It carries the rules of the deleted plan-critique
// skill (feat-0be892a9: the skill was never loaded while the command ran
// eight times, so the rules now ship inside the briefs).

const designBriefHead = `# Design critique brief

You are the design critic. You review a drafted wipnote plan as a design
document. You did not write it. Read it fresh.

## Paraphrase first

Paraphrase first. Before you judge anything, write the plan back in your own
words: the problem, the goal, and what each slice delivers. If you cannot,
that is your first finding.

## What to look for

- Missing slices and scope gaps.
- Unclear acceptance criteria. A done_when line must be checkable.
- Conflicting goals or constraints.
- Undefined states and edge cases.
- Whether a reviewer can understand the plan and approve it.
`

const feasibilityBriefHead = `# Feasibility critique brief

You are the feasibility critic. You review a drafted wipnote plan as an
implementation contract. You did not write it. Read it fresh.

## What to look for

- Cited files exist and the line ranges match.
- Existing patterns are reused.
- Each done_when line is testable from the files the slice lists.
`

// feasibilityChecks holds the checks of the old skill plus three added ones.
const feasibilityChecks = `
## Checks before you score a slice

- Existing helper: for each function or type a slice adds, search the repo for
  one that already does the job. Name it by path:line.
- Design choice left to the implementer: list each place where a decision or
  done_when line leaves a choice open. An implementer must not have to guess.
- New files: a file marked (new) in the reading list does not exist yet. Do not
  flag it as missing.
- External claims: verify library behaviour, SDK contracts, API shapes and
  harness hook semantics against current official docs with web search or web
  fetch. Unverified is warn. Contradicted is danger.
- Research URLs: check that every cited research URL resolves and supports its
  claim. Dead or unsupported is warn. Contradicted is danger. Re-check old
  accessed dates on version-sensitive claims.
- Research gate: run wipnote plan validate-yaml <plan-id>. A v4 standard or
  complex slice with neither research nor research_waiver is danger. So is a
  plan with no design.research.
- Reinvented wheels: when a maintained package already does the job, flag it as
  warn and cite the package and its URL.
- Backend claims: check agy flags with agy --help and model labels with agy
  models. Check Codex with its official docs or codex exec --help. Check Ollama
  with its docs or ollama --help. If you cannot verify a claim, mark it
  unverified. Never mark it verified.
`

const findingsFormat = `
## How to report

Give your findings in your reply, one per line. Each line has:

- the slice num (or "plan" for the whole plan)
- a severity: success, warn, danger or info
- a one-line summary
- evidence: a source URL, "runtime: <command>", or "unverified: <reason>"

danger must be fixed before review. warn should be.
Start the reply with the word "completed" if you read every file on the list.
Start it with "not_run" if you could not.
`

// planCritiqueLong is the help text of plan critique. It holds the author's
// steps from the deleted plan-critique skill.
const planCritiqueLong = `Write the critic briefs for a plan and print how to start the critics.

The command writes design-brief.md and feasibility-brief.md into
.wipnote/plans/<plan-id>.critique/. Each brief holds the plan id, a reading
list and any settled risks. It holds no drafting history. The JSON it prints
keeps plan_id, title, status, complexity, slice_count and critique_warranted,
and adds briefs and spawn. spawn has one wipnote delegate spawn line per
critic, on the review profile. The command starts no worker. Run both lines.

Run it after validation and before human review. Skip it for a one-slice plan.
Plans under 3 slices print critique_warranted=false. It is required when any
slice is complexity: complex.

Steps after the critics reply:
  1. Record each finding with the slice num, a severity (success, warn, danger,
     info), a one-line summary and evidence (a URL, "runtime: <command>" or
     "unverified: <reason>"). danger must be fixed before review.
  2. Fix a slice in place: wipnote plan critique revise <plan-id> --slice N
     --what "..." --why "..." --tests "...". Then run wipnote plan validate-yaml.
  3. Store the critique with wipnote plan set-critique-yaml <plan-id> --data
     '<json>'. Shape: reviewed_at, reviewers, assumptions (id, status, text,
     evidence), critics (title, status completed or not_run, sections of
     heading and items of badge, kind, text), risks (risk, severity, mitigation),
     accepted_risks (text, reason) and synthesis. Mark a disputed finding
     "contested": true and give "citation" as path:line or an http(s) URL.
  4. Critique stops after 2 rounds. A third run is refused and names the open
     findings. Accepted risks stay settled and are not raised again.
  5. Hand the plan to review when no danger finding is open.

Example:
  wipnote plan critique plan-a1b2c3d4`
