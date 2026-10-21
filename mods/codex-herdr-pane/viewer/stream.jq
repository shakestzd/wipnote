# Turns `claude -p --output-format stream-json` lines into readable output for the pane.
#   jq --unbuffered -R -r -f stream.jq                 text
#   jq --unbuffered -n -R -r --arg mode md -f stream.jq      Markdown, a tool call and its result paired by
#                                                            tool_use_id and shown together, in one fold, when the result arrives
#                                                            (a call never answered ends as "(no result)")
#   ... --arg mode mdlive                                    the same, but a call still waiting is held back, not
#                                                            flushed (a log still being written); md needs -n (inputs)
#   jq -R -r --arg mode pane -f stream.jq                    view, but a line that is not JSON (stderr, text
#                                                            output) passes as it is: a headless claude in a pane
#   jq -n -R -r --arg mode answer -f stream.jq               the last result event's text (what claude -p prints); none when empty
#   jq -n -R -r --arg mode usage -f stream.jq                the tokens and cost of every result event, one line
#   jq -n -R -r --arg mode end -f stream.jq                  both in one pass: the usage line (empty when there
#                                                            is no result), then the answer when there is one
# Each step only appends to the output, so the output for the first N lines is a prefix of
# the output for more lines: a follower can re-render and append just the new bytes.
# fromjson? skips partial lines. `events` is the single parse into neutral items
# ({k: say|call|result|done, ...}); `txt` and `md` only format them.
include "md" {search: "./"};
def clip($n): gsub("\\s+"; " ") | if length > $n then .[:$n] + "..." else . end;
def summary: ( .command // .file_path // .path // .pattern // .url // .description // .prompt
               // (tojson) ) | tostring | clip(100);
def body: if type == "array" then map(.text? // "") | join(" ") else (. // "") | tostring end;
def events:
  if .type == "assistant" then
    (.message.content // [])[]
    | if .type == "text" then {k: "say", text: .text}
      elif .type == "tool_use" then {k: "call", id: .id, name: .name, arg: (.input | summary)}
      else empty end
  elif .type == "user" then
    (.message.content // []) | if type == "array" then .[] else empty end
    | select(.type == "tool_result")
    | {k: "result", id: .tool_use_id, err: (.is_error // false), text: (.content | body)}
  elif .type == "result" then
    {k: "done", subtype: (.subtype // "result"), err: (.is_error // false), secs: ((.duration_ms // 0) / 1000 | floor), turns: (.num_turns // 0),
     in: (.usage.input_tokens // 0), out: (.usage.output_tokens // 0), cost: .total_cost_usd}
  else empty end;
def txt:
  if .k == "say" then .text
  elif .k == "call" then "> \(.name): \(.arg)"
  elif .k == "result" then "  = \(.text | clip(200))\(if .err then " (error)" else "" end)"
  elif .k == "done" then "--- \(.subtype)\(if .err then " (error)" else "" end) in \(.secs)s, \(.turns) turns, \(.in) in / \(.out) out tokens\(if .cost then ", $\(.cost)" else "" end) ---"
  else empty end;
def block: (. as $r | $r.text | result(1500; $r.err; ""));
def resulttail: " · " + (.text | resultsize) + (if .err then " · error" else "" end);
def md:
  if .k == "say" then ("### Assistant\n\n" + (.text | prose) + "\n")
  elif .k == "call" then (toolline(.name; .arg) + "\n")
  elif .k == "result" then fold("result" + resulttail; .err; block) + "\n"
  elif .k == "done" then
    "*\(.subtype | esc)\(if .err then " (error)" else "" end) in \(.secs)s, \(.turns) turns, \(.in) in / \(.out) out tokens\(if .cost then ", $\(.cost)" else "" end)*\n"
  else empty end;
def callline: toolline(.name; .arg);
# A call and its result as one collapsible section (md.jq's fold): a summary title, then the label and result
def callfold($r): fold("\(.name): \(.arg)" + ($r | resulttail); $r.err; callline + "\n\n" + ($r | block));
def pend_at($id): (.pend | map(.id) | index($id));
# One step of the fold: .pend holds calls waiting for their result, .out what this step prints.
def step($it; $flush):
  .out = null
  | if $it.k == "call" and ($it.id // "") != "" then .pend += [$it]
    elif $it.k == "result" and ($it.id // "") != "" and pend_at($it.id) != null then
      pend_at($it.id) as $i
      | .out = ((.pend[$i] | callfold($it)) + "\n")
      | .pend |= (.[:$i] + .[$i + 1:])
    elif $it.k == "eof" then
      .out = (if $flush and (.pend | length) > 0 then (.pend | map(callline + "\n\n*(no result)*\n") | join("\n")) else null end)
      | .pend = []
    else .out = ($it | [md] | first // null) end;
def mdstream($flush):
  foreach ((inputs | (try fromjson catch null) | select(type == "object") | events), {k: "eof"}) as $it
    ({pend: [], out: null}; step($it; $flush); .out | select(. != null));
def results: [inputs | (try fromjson catch null) | select(type == "object" and .type == "result")];
def total(f): map(f // 0) | add;
def answer: last | .result? // empty | strings | select(. != "");  # an empty answer is none: the report shows the log
def usage: select(length > 0) | total(.total_cost_usd) as $c
  | "\(total(.usage.input_tokens)) in / \(total(.usage.output_tokens)) out tokens\(if $c > 0 then ", $\($c * 10000 | round / 10000)" else "" end)";
($ARGS.named.mode // "view") as $mode
| if $mode == "md" then mdstream(true)
  elif $mode == "mdlive" then mdstream(false)
  elif $mode == "pane" then (. as $l | (try fromjson catch null) as $j | if ($j | type) == "object" then ($j | events | txt) else $l end)
  elif $mode == "answer" then (results | answer)
  elif $mode == "usage" then (results | usage)
  elif $mode == "end" then (results | ((usage // ""), answer))
  else (fromjson? | events | txt) end
