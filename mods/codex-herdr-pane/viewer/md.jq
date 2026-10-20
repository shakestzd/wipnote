# Markdown building blocks shared by codex.jq and stream.jq (their `md` mode).
# The one place that knows how to put untrusted text into Markdown safely:
#   fenced   a code fence longer than any backtick run inside, so output cannot break out
#   code     an inline code span, delimiter sized the same way
#   esc      backslash-escapes Markdown punctuation in text that must stay inline
#   prose    assistant text as written, with any fence it leaves open closed again
#   fold     a tool result wrapped in the markers the Live tab shows as a collapsible section
# Every function returns one complete block (opened and closed), so a stream cut
# between two events never leaves a dangling fence.
def collapse: gsub("\\s+"; " ");
def cut($n): if length > $n then .[:$n] + "..." else . end;
def plain: gsub("\u001b\\[[0-9;?]*[A-Za-z]"; "") | gsub("\r"; "");
def ticks: [match("`+"; "g").length] | (max // 0);
def fenced($info): . as $t | ("`" * ([3, ($t | ticks) + 1] | max)) as $f | "\($f)\($info)\n\($t)\n\($f)";
def fenced: fenced("");
def code: collapse | . as $t | ("`" * (($t | ticks) + 1)) as $d
  | if ($t | test("^`|`$")) then "\($d) \($t) \($d)" else "\($d)\($t)\($d)" end;
def esc: gsub("(?<c>[\\\\`*_\\[\\]<>#|~&])"; "\\\(.c)");
def toolline($name; $arg):
  "**\($name | gsub("[^A-Za-z0-9_.:-]"; ""))**" + (if ($arg // "") != "" then " " + ($arg | code) else "" end);
def errquote($msg): "> **error**" + (if ($msg // "") != "" then " " + ($msg | collapse | cut(300) | esc) else "" end);
# The macOS developer-tools errors that `git` and friends print inside Codex's read-only
# sandbox (xcrun cannot write its cache). Each is anchored to the whole line, so output that
# merely mentions xcrun, confstr or /tmp is never matched. The one list of them.
def xcrun_noise: [
  "^[A-Za-z0-9_.-]+: warning: confstr\\(\\) failed with code [0-9]+: couldn't get path of DARWIN_USER_[A-Z_]+; using /tmp instead$",
  "^[A-Za-z0-9_.-]+: error: couldn't create cache file '/tmp/xcrun_db-[^']*' \\(errno=[^)]*\\)$",
  "^([0-9-]+ [0-9:.]+ )?xcodebuild\\[[0-9]+:[0-9]+\\] +DVTFilePathFSEvents: Failed to start fs event stream\\.$",
  "^([0-9-]+ [0-9:.]+ xcodebuild\\[[0-9]+:[0-9]+\\] )?\\[MT\\] DVTDeveloperPaths: Failed to get length of DARWIN_USER_CACHE_DIR from confstr\\(3\\), error = "
];
def is_xcrun_noise: . as $l | any(xcrun_noise[]; . as $p | $l | test($p));
def xcrun_note: "*macOS xcrun cache errors hidden (sandbox), see raw.log*";
# A result block: an optional error quote, then the body in a fence (xcrun noise lines moved
# out of it into one italic note after it), cut to $max characters.
def result($max; $isErr; $msg):
  (plain | sub("\n+$"; "") | split("\n")) as $all
  | ($all | map(select(is_xcrun_noise | not))) as $kept
  | (($all | length) > ($kept | length)) as $hid
  | ($kept | join("\n") | sub("\n+$"; "") | if $hid and . == "" then "(no output)" else . end
     | if length > $max then .[:$max] + "\n... [truncated, \(length - $max) more characters]" else . end) as $body
  | (if $isErr then errquote($msg) + "\n\n" else "" end) + ($body | fenced) + (if $hid then "\n\n" + xcrun_note else "" end);
# Closes a fence that a prose block opens and never closes (CommonMark: an opening fence is
# 3+ backticks or tildes, indented under 4 spaces, a backtick fence's info has no backtick;
# it closes on a line of the same character, at least as long, and nothing else).
def fence_open: capture("^ {0,3}(?<f>`{3,}|~{3,})(?<i>.*)$")?
  | select((.f | startswith("~")) or (.i | contains("`") | not)) | {c: .f[:1], n: (.f | length)};
def fence_step($l): . as $s |
  if . == null then ($l | fence_open) // null
  elif ($l | test("^ {0,3}" + $s.c + "{" + ($s.n | tostring) + ",}\\s*$")) then null
  else . end;
# The fold markers around a tool result: the Live tab (codex_view.py) shows what lies between
# them (the call's label, then its result) as one collapsible section titled by the marker. An HTML comment, so any other Markdown
# reader hides the markers. The title is one line that cannot end the comment early; the body
# is a complete block, so a fold never holds a fence open past its end marker. A failed result
# opens expanded. The viewer reads markers only outside fences, and prose (assistant text) has
# any marker-like line escaped, so only these functions can make one.
def foldtitle: plain | collapse | cut(300) | gsub("--+>"; "->");
def fold($title; $open; $body):
  "<!-- fold \(if $open then "open" else "closed" end): \($title | foldtitle) -->\n\($body)\n<!-- /fold -->";
def is_foldmark: test("^ {0,3}<!-- ?/?fold");
# How many lines a result shows (xcrun noise not counted), for a fold's title.
def resultsize:
  (plain | sub("\n+$"; "") | split("\n") | map(select(length > 0 and (is_xcrun_noise | not))) | length) as $n
  | if $n == 0 then "no output" elif $n == 1 then "1 line" else "\($n) lines" end;
def prose:
  . as $t
  | (reduce ($t | split("\n"))[] as $l ({s: null, o: []};
      .o += [if .s == null and ($l | is_foldmark) then ($l | sub("<!--"; "\\<!--")) else $l end]
      | .s = (.s | fence_step($l)))) as $r
  | ($r.o | join("\n")) + (if $r.s == null then "" else "\n" + ($r.s.c * $r.s.n) end);
def passthrough: select(length > 0) | plain | fenced("text");
# Markdown-only treatment of the stderr passthrough: the "Reading additional input from
# stdin" line is dropped, clean.awk's one collapsed MCP warning becomes an italic note,
# anything else stays in a text fence. (clean.awk dedupes the warning per run.)
def stdin_line: test("^\\s*Reading additional input from stdin");
def mdline:
  if test("^! an MCP server was unreachable") then "*MCP server unreachable, see raw.log*"
  elif stdin_line then empty
  else passthrough end;
# Strips a `sh -c`-style wrapper (bash/zsh/sh/dash/ksh, any path, flags such as -lc) and its
# quote layer from a command. The wrapped word is read the way the outer shell reads it:
# single quotes literal; double quotes unescape \\ \" \$ \` only; unquoted text unescapes any
# backslash pair; adjacent quoted pieces join. Anything that would need the outer shell to
# expand something (an unescaped $ or backtick in double quotes, unquoted metacharacters or
# whitespace, an unterminated quote) is not guessed at: the original text is kept.
def sw_step($c):
  if .m == "s" then (if $c == "'" then .m = "u" else .w += $c end)
  elif .m == "d" then
    if .e then (.e = false | if ($c | IN("\"", "\\", "$", "`")) then .w += $c
                              elif $c == "\n" then .bad = true else .w += "\\" + $c end)
    elif $c == "\\" then .e = true
    elif $c == "\"" then .m = "u"
    elif ($c | IN("$", "`")) then .bad = true
    else .w += $c end
  elif .e then (.e = false | if $c == "\n" then .bad = true else .w += $c end)
  elif $c == "\\" then .e = true
  elif $c == "'" then .m = "s"
  elif $c == "\"" then .m = "d"
  elif ($c | test("[\\s$`*?\\[\\]{}()<>|&;~!#]")) then .bad = true
  else .w += $c end;
def shellword:
  reduce split("")[] as $c ({m: "u", w: "", e: false, bad: false}; sw_step($c))
  | if .m == "u" and (.e | not) and (.bad | not) and (.w | length) > 0 then .w else null end;
def unwrap_shell:
  . as $orig
  | (capture("^\\s*(?:\\S*/)?(?:bash|zsh|sh|dash|ksh)\\s+-[a-z]*c[a-z]*\\s+(?<rest>\\S.*?)\\s*$"; "s") // null) as $m
  | if $m == null then $orig else ($m.rest | shellword) // $orig end;
