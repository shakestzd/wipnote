---
type: llm
---

PASS if the plan runs `wipnote feature start feat-9abc1234` before writing any tests or code, writes failing tests before the implementation, runs the full quality gate after implementing, commits with feat-9abc1234 in the commit subject or a trailer, and finishes with `wipnote feature complete feat-9abc1234` (reporting rather than retrying if it is refused). Loading skills or reading code before the tests is fine.
FAIL if the implementation comes before the tests, or if the start, commit, or complete step is missing.
