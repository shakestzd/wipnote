# Joins neighbouring text fences in clean.md. codex.jq fences each plain line on its own
# (jq reads one line at a time), so a text-mode log such as codex review became one fence per
# line. A closing fence, a blank line and an opening "<same fence>text" are dropped, which
# runs the two blocks together. Only fences of the same length are joined, and each part's
# backtick runs are already shorter than its fence, so the joined block cannot be broken out of.
function flush() { if (held != "") { print held; if (blank) print ""; held = ""; blank = 0; fflush() } }
held != "" && !blank && $0 == ""       { blank = 1; next }
held != "" && blank && $0 == held "text" { held = ""; blank = 0; next }
                                       { flush() }
/^`{3,}$/                              { held = $0; blank = 0; next }
                                       { print; fflush() }
END                                    { flush() }
