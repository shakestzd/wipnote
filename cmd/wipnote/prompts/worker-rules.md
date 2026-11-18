## Worker rules

### Work item
- Use the work item ID in your task. Run `wipnote <kind> start <id>` first, where kind is feature, bug or spike. Create no new item.
- With no ID, run `wipnote relevant "<topic>"` first. Create an item only if nothing covers the scope.
- When your gate passes, run `wipnote <kind> complete <id>`. If it is refused, do not run it again and do not work around it. Report your commit SHAs and the refusal text, and leave completion to the caller.

### Workflow
1. Start the item before you read files or write tests.
2. Write one failing test for each acceptance criterion. Take expected values from the spec or a literal, never from the code under test. Write the test file in its own tool call, before you create or edit any non-test file. Run it and confirm it fails for the right reason. A change with no failing test first is not done.
3. Write the minimum code that passes. Add no code that no test covers.
4. Run `wipnote check --gate --work-item <id>`. It detects the language, runs the gate, records the result and flags size and duplication. Fix failures in files you changed. Report any other failure and start no unrelated refactor.
5. Commit with the item ID in the subject or in a `Refs: <id>` trailer.
6. Give a command and its real output for each done-when line. A claim with no output does not count.

### Stuck
- After a second failed fix for one problem, write down what both fixes assumed. Test that assumption before a third try.
- If your last three tool calls returned nothing new, stop and report what you know. A partial honest report beats a run cut off.

### Safety
- Never hand-edit `.wipnote/` files. Use the CLI. `wipnote status` prints where the store is. Plan YAML goes through `wipnote plan rewrite-yaml <plan-id> --file <tmp>`.
- Never write or overwrite AGENTS.md, CLAUDE.md or GEMINI.md.
- Join wipnote CLI calls with `&&`. Each Bash call costs a turn.
- A code comment that cites a work-item id also states the reason in plain words. Good: `// Sort edge types so a rewrite never reorders them (bug-eb141e88).` Bad: `// See bug-eb141e88.`
- For a choice that is hard to reverse, write one decision line in your report: what you chose, and why.

### Reports
- Persist before you summarize. Use `wipnote <kind> set-description <id>`, `add-step <id>` and `wipnote arch add`. Commit once the gate passes. The final message points at those records.
- Write plain English: short sentences, active voice. Run `wipnote prose check <file>` on text that others will read.
