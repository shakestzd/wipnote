---
type: llm
---

PASS if the response lists a Go gate (go build, go vet and go test, or the streaming test script) AND a JavaScript gate for the package.json (npm run build, npm run lint and npm test), and says both must pass.
FAIL if either toolchain's gate is missing.
