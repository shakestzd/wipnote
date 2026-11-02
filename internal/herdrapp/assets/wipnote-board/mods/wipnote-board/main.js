import { View, div, svg, image } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { agents, exec, run, setAgentOrder, spaces, terminals, theme } from "herdr";

// One line per agent, grouped by what it needs from you, with the reason it is
// working: the wipnote work item its terminal is labelled with, and its
// delegate run. wipnote labels a terminal wipnote.title ("feat-1234: Title")
// and a worker wipnote.worker. See RECIPE.md.
const GROUPS = [
  { status: "blocked", label: "Needs you", color: (t) => t.blocked },
  { status: "error", label: "Failed", color: (t) => t.blocked },
  { status: "working", label: "Working", color: (t) => t.working },
  { status: "done", label: "Done", color: (t) => t.done },
  { status: "idle", label: "Idle", color: (t) => t.idle },
];

const POLL_MS = 5000; // run state only changes when a worker does; poll gently

export default class WipnoteBoard extends View {
  init(_props, cx) {
    this.runs = {}; // terminal address -> delegate run row
    this.refresh(cx);
    cx.timer.every(POLL_MS, (cx) => this.refresh(cx));
  }

  // Each folder with a space is asked once. A folder with no .wipnote/ fails
  // the call, which only means it has no runs. Reads run from outside any
  // terminal, so wipnote reads files and leaves the workers alone.
  refresh(cx) {
    cx.spawn(async (cx) => {
      const runs = {};
      const folders = new Set(spaces().filter((s) => !s.machine && s.folder).map((s) => s.folder));
      for (const folder of folders) {
        try {
          const r = await exec("wipnote", ["delegate", "list", "--json", "--project-dir", folder], { cwd: folder });
          if (r.code !== 0) continue;
          for (const row of JSON.parse(r.stdout) ?? []) {
            if (row.pane) runs[row.pane] = row;
          }
        } catch (_e) {
          // wipnote is not on this PATH, or the reply is not JSON: show no runs.
        }
      }
      this.runs = runs;
      cx.notify();
    });
  }

  row(agent, group, labels) {
    const t = theme();
    const run_ = this.runs[agent.address];
    const why = labels[agent.address]?.["wipnote.title"] ?? agent.tab_name;
    const who = run_?.name ?? labels[agent.address]?.["wipnote.worker"] ?? agent.program.name;
    const icon = agent.program?.icon
      ? (agent.program.tint ? svg(agent.program.icon).text_color(t.foreground) : image(agent.program.icon))
      : div();
    return h_flex().id(`agent-${agent.address}`).w_full().flex_none().gap(8).px(10).py(6).items_center()
      .cursor_pointer().when(agent.focused, (el) => el.bg(t.selected)).hover((s) => s.bg(t.hover))
      .on_click(() => run("agent.focus", { terminal: agent.address }))
      .child(div().w(3).h(30).rounded(2).flex_none().bg(group.color(t)))
      .child(icon.size(16).flex_none())
      .child(v_flex().flex_1().min_w(0)
        .child(div().text_size(12).text_color(t.foreground).truncate().child(why))
        .child(div().text_size(11).text_color(t.muted).truncate().child(`${who} · ${agent.space_name}`)));
  }

  render() {
    const t = theme();
    const labels = {};
    for (const term of terminals()) labels[term.address] = term.labels ?? {};
    const list = agents();
    const groups = GROUPS.map((group) => ({ group, members: list.filter((a) => a.status === group.status) }))
      .filter((g) => g.members.length > 0);
    setAgentOrder(groups.flatMap((g) => g.members.map((a) => a.address)));
    return v_flex().id("wipnote-board").size_full().bg(t.chrome).overflow_y_scrollbar().pb(8)
      .child(div().px(10).pt(10).pb(6).text_size(11).font_semibold().text_color(t.muted).child("WORK"))
      .when(groups.length === 0, (el) => el.child(div().px(10).text_size(12).text_color(t.dim).child("No agents are running.")))
      .children(groups.map(({ group, members }) => v_flex().w_full().flex_none()
        .child(h_flex().px(10).pt(8).pb(2).gap(6).text_size(11).text_color(t.muted)
          .child(div().font_semibold().child(group.label))
          .child(div().text_color(t.dim).child(String(members.length))))
        .children(members.map((a) => this.row(a, group, labels)))));
  }
}
