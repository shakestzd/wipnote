package main

// Step rules printed by `wipnote plan next`. Each text stays under 40 lines
// with its header. They came from the retired planning skill (feat-bcc2297f). The vertical
// slice and small-change rules are plan-2622b3f7 card D5.

const planNextTriage = `No plan id given. Triage first.
- Change of one or two files: no plan. Make it directly under a feature item.
- Work that needs design first: write a plan. Classify each slice:
  trivial  no interview. Needs only title, why, files, effort, risk.
  standard requirements, scope and done-when. Needs what, decisions_notes (50+ chars), one block or blocks_waiver.
  complex  all four interview stages. As standard, plus 2+ done_when and one answered slice question.
- Ask the owner unless the request makes the class clear.
- Cut slices vertically: each one crosses every layer it needs and works alone.
  Do not cut by layer (all schema, then all UI).
Create the plan:
  wipnote plan create-yaml "<title>" --description "<description>" --track <trk-id>
Then run: wipnote plan next <plan-id>
`

var planNextRules = map[string]string{
	"Draft":     planNextDraft,
	"Validate":  planNextValidate,
	"Critique":  planNextCritique,
	"Review":    planNextReview,
	"Decisions": planNextDecisions,
	"Promote":   planNextPromote,
	"Spec":      planNextSpec,
	"Close":     planNextClose,
}

const planNextDraft = `The plan has no slices. Run triage first: wipnote plan next (no id) prints the rule.
Research live before drafting: official docs, maintained packages, wipnote relevant <topic>.
  v4 gate: design.research has 1+ source. Each standard or complex slice has research: or research_waiver:.
  A slice that adds a module needs adopt_vs_build (decision adopt with package, or build with rejected).
Interview: one stage per ask-user call, or one chat message where there is no ask-user tool.
  wipnote plan interview-questions <plan-id> [<slice-num>] prints the questions and block prompts.
  Stages: requirements, scope and state, contract (complex only), done-when.
  Where you cannot ask, mark inferred answers **Inferred:** in decisions_notes.
Cut slices vertically: each one crosses the layers it needs and works alone.
Blocks: run wipnote plan blocks for the live catalog. Ground every field, route and path.
  Wireframes use var(--wf-*) tokens. With nothing to ground on, add blocks_waiver: <reason>.
what holds 56 words at most. Put rationale in decisions_notes under **Trigger:** **State:** **Payload:** **Rejected:**.
A trivial slice needs only id, num, title, why, files, effort, risk, complexity.
In native plan mode, present the outline with ExitPlanMode, then write the YAML.
Write it:
  wipnote plan create-yaml "<title>" --description "<description>" --track <trk-id>
  wipnote plan rewrite-yaml <plan-id> --file <path>
Then run: wipnote plan next <plan-id>
`

const planNextValidate = `Fix every error. Each one names the field and the fix.
Write the YAML back: wipnote plan rewrite-yaml <plan-id> --file <path>
Check again: wipnote plan validate-yaml <plan-id>
Missing decisions_notes on an older plan: wipnote plan elicit-decisions, or set complexity: trivial for a one-shot patch.
To add visual blocks to an existing plan:
  1. wipnote plan blocks lists the types and required fields. Do not hardcode them.
  2. Copy the YAML to a temp file and add blocks: entries. Keep all other fields.
  3. wipnote plan rewrite-yaml <plan-id> --file <tmp>, then wipnote plan validate-yaml <plan-id>.
  4. wipnote plan render <plan-id> writes the HTML. Report a render error as it is.
Never write .wipnote/plans/ directly.
Then run: wipnote plan next <plan-id>
`

const planNextCritique = `The plan has 3+ slices or a complex slice, so it needs a critique before review.
Run wipnote plan critique <plan-id>. It writes a design brief and a feasibility brief and prints a delegate spawn line for each. Run both lines. Each critic reads the plan in its own context.
Record the findings with wipnote plan set-critique-yaml <plan-id>. Critique stops after two rounds.
Resolve every danger finding.
If a finding changes a slice's design, re-author its block, re-derive the prose, and validate again.
Then run: wipnote plan next <plan-id>
`

const planNextReview = `Get a decision on every listed slice from the owner.
Run wipnote plan slices <plan-id> first. The wipnote-plan-review pane then tracks each decision.
Read the cards in the plan YAML in the store. wipnote plan show prints only metadata.
Show each slice: title, what, done_when, blocks, open questions.
Ask approve, request changes or reject. Use the ask-user tool where there is one.
Record each answer:
  wipnote plan approve-slice <plan-id> <num>
  wipnote plan reject-slice <plan-id> <num> [--changes-requested]
Revise each changes_requested slice from the comments, then validate again.
Then run: wipnote plan next <plan-id>
`

const planNextDecisions = `Each listed slice is approved and needs its decisions before promotion.
1. Read the card: wipnote plan show <plan-id>. If decisions_notes exists, ask whether to redo, edit or keep it.
2. Ask three questions in one ask-user call or one chat message:
   Scope: what is in and out?  Decisions: which choices, and why?  Context: constraints, related work, file ownership?
   For an external library or API, ground the options in current docs first.
3. Write them:
   wipnote plan elicit-decisions <plan-id> <num> --scope "..." --decisions "..." --context "..."
Where there is no ask-user tool, list the questions in chat. Label inferred answers **Inferred:**.
Then run: wipnote plan next <plan-id>
`

const planNextPromote = `Each listed slice is approved and has decisions.
  wipnote plan promote-slice <plan-id> <num>
Fix whatever it names if it refuses. Use --waive-deps only when the owner accepts it.
Then run: wipnote plan next <plan-id>
`

const planNextSpec = `Each listed feature has no spec yet.
1. wipnote spec generate <feature-id> --insert
   It refuses to overwrite a spec and prints a diff. Pass --force only when the owner accepts it.
2. wipnote compliance <feature-id> must list criteria. If it lists none, fix the decisions and generate again.
decisions_notes goes verbatim into the spec's Decisions section.
--allow-spec-skip is an emergency override only.
Report in 2 or 3 lines: what changed, where the spec went, the next step.
Then run: wipnote plan next <plan-id>
`

const planNextClose = `Specs are in place. Run the slices with /wipnote:execute <feature-id>.
Slices still open are listed above. When every slice is done:
  wipnote plan set-status <plan-id> completed
`
