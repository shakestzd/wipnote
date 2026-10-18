# Cleans a Codex log for the pane view and for the text returned to Claude.
# One implementation of the noise rules; the raw log keeps everything.
# -v seen=FILE  makes the MCP note once per FILE (a run directory spans several codex
#               processes in a chained call); without it, once per process.
function first_mcp(   x) {
  if (seen == "") return !mcp++
  if ((getline x < seen) >= 0) { close(seen); return 0 }
  print "1" > seen; close(seen); return 1
}
/^hook: /                              { next }
/^warning: clamping .* hook timeout/   { next }
/^[0-9T:.Z-]+ +(INFO|DEBUG|WARN) /     { next }
/rmcp::transport::worker/ {
  if (first_mcp()) { print "! an MCP server was unreachable (rmcp transport closed); raw.log has the detail"; fflush() }
  next
}
{ print; fflush() }
