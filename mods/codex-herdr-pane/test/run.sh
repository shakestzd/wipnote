#!/usr/bin/env bash
# All behavioural tests: jq cases, pane simulation with stubs, headless Textual Live tab.
T="$(cd "$(dirname "$0")" && pwd)"; REQ='textual>=8.2,<9'; rc=0
(cd /tmp && uv run --quiet --no-project --with "$REQ" python "$T/md_cases.py") || rc=1
bash "$T/pane_sim.sh" || rc=1
(cd /tmp && uv run --quiet --no-project --with "$REQ" python "$T/live_test.py") || rc=1
exit $rc
