# Joins neighbouring text fences in clean.md. codex.jq fences each plain line on its own
# (jq reads one line at a time), so a text-mode log such as codex review became one fence per
# line. When a text block closes and, after one blank line, a text block of the same fence opens,
# the closing fence, the blank line and the opening fence are dropped, which runs the two blocks
# together. Only text blocks are joined: another block (a command result, a code block) keeps its
# closing fence. Each part's backtick runs are already shorter than its fence, so the joined
# block cannot be broken out of.
function flush() { if (held != "") { print held; if (blank) print ""; held = ""; blank = 0; fflush() } }
held != "" && !blank && $0 == ""         { blank = 1; next }
held != "" && blank && $0 == held "text" { open = held; text = 1; held = ""; blank = 0; next }
                                         { flush() }
open == "" && /^`{3,}/                   { open = $0; sub(/[^`].*$/, "", open); text = ($0 == open "text"); print; fflush(); next }
open != "" && $0 == open                 { open = ""; if (text) { held = $0; blank = 0; next } print; fflush(); next }
                                         { print; fflush() }
END                                      { flush() }
