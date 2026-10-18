# Renders `codex exec --json` events for the pane, and is the one place the token
# split is parsed.
#   jq --unbuffered -R -r -f codex.jq                      text (non-JSON lines pass through)
#   jq --unbuffered -R -r --arg mode md -f codex.jq        Markdown (a command's label and result together, when it completes;
#                                                           non-JSON lines in a text fence, stdin line dropped, MCP warning one italic note)
#   jq --unbuffered -R -r --arg mode mdraw -f codex.jq     every line (even JSON) through mdline: the rest of a chained command
#   jq -R -r --arg mode rest -f codex.jq                   plain text with the stdin line dropped (text returned to Claude)
#   jq -R -r --arg mode usage -f codex.jq raw.log          only the usage split
# `events` is the single parse of an event into neutral items ({k: say|think|call|result|
# err|turn|done, ...}); `txt` and `md` only format them. Event shapes: thread/turn/item
# events as printed by codex-cli 0.163.0. agent_message, error and turn.completed were
# read from real runs; the other item kinds follow the documented item names, and
# unknown ones are shown by name rather than dropped.
include "md" {search: "./"};
def clip($n): gsub("\\s+"; " ") | if length > $n then .[:$n] + "..." else . end;
def commas: if . < 1000 then tostring
  else ((. / 1000 | floor) | commas) + "," + ((. % 1000 | tostring) | ("000" + .)[-3:]) end;
def split: "input \(.input_tokens // 0 | commas) (cached \(.cached_input_tokens // 0 | commas)), output \(.output_tokens // 0 | commas)";
def reasoning: if (.reasoning_output_tokens // 0) > 0 then ", reasoning \(.reasoning_output_tokens | commas)" else "" end;
def item:
  .item as $i
  | if $i.type == "agent_message" then {k: "say", text: $i.text}
    elif $i.type == "error" then {k: "err", pre: "", text: ($i.message // "error")}
    elif $i.type == "reasoning" then {k: "think", text: ($i.text // "")}
    elif $i.type == "command_execution" then
      if .type == "item.started" then {k: "call", name: "Bash", bare: true, arg: ($i.command // "")}
      else {k: "result", command: $i.command, code: ($i.exit_code // "?"), err: (($i.exit_code // 0) != 0), text: ($i.aggregated_output // "")} end
    elif .type == "item.completed" then {k: "call", name: ($i.type // "item"), arg: $i.path}
    else empty end;
def events:
  if .type == "item.started" or .type == "item.completed" then item
  elif .type == "turn.started" then {k: "turn"}
  elif .type == "turn.completed" then {k: "done", usage: .usage}
  elif .type == "turn.failed" then {k: "err", pre: "turn failed: ", text: (.error.message // "unknown")}
  elif .type == "error" then {k: "err", pre: "", text: (.message // "error")}
  else empty end;
def txt:
  if .k == "say" then .text
  elif .k == "err" then "! \(.pre)\(.text | clip(300))"
  elif .k == "think" then "(thinking) \(.text | clip(200))"
  elif .k == "call" then "> \(if .bare then .arg | clip(160) else .name + (if .arg then " " + .arg else "" end) end)"
  elif .k == "result" then "  = exit \(.code): \(.text | clip(200))"
  elif .k == "done" then "--- done: \(.usage | split)\(.usage | reasoning) ---"
  else empty end;
def md:
  if .k == "say" then ("### Assistant\n\n" + (.text | prose) + "\n")
  elif .k == "err" then (errquote(.pre + .text) + "\n")
  elif .k == "think" then ("**thinking**\n\n" + (.text | clip(200) | fenced) + "\n")
  elif .k == "call" and .bare then empty  # a command is shown once it completes, label and result together
  elif .k == "call" then (toolline(.name; .arg) + "\n")
  elif .k == "result" then
    (. as $r | (if $r.command != null then toolline("Bash"; $r.command | unwrap_shell | cut(160)) + "\n\n" else "" end)
      + ($r.text | result(1500; $r.err; "exit \($r.code)"))) + "\n"
  elif .k == "turn" then "### Turn\n"
  elif .k == "done" then "*\(.usage | split)\(.usage | reasoning)*\n"
  else empty end;
. as $line
| (try fromjson catch null) as $e
| ($ARGS.named.mode // "view") as $mode
| if $mode == "usage" then
    (select($e | type == "object" and .type == "turn.completed") | $e.usage | split + reasoning)
  elif $mode == "mdraw" then ($line | mdline + "\n")  # lines that are never events: the rest of a chained command
  elif $mode == "rest" then ($line | select(stdin_line | not))  # the same noise rules as plain text (the text returned to Claude)
  elif ($e | type) == "object" then ($e | events | if $mode == "md" then md else txt end)
  elif $mode == "md" then ($line | mdline + "\n")
  else $line end
