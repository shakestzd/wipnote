import { View, div, svg, image } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { agents, exec, run, setAgentOrder, spaces, terminals, theme } from "herdr";

// A tree of agents grouped by what each needs from you. An agent that has live
// agent children is a delegator: its row, then a "workers" row with the count,
// then one row per worker. Every other agent is a flat row. wipnote labels a
// terminal wipnote.title ("feat-1234: Title") and a worker wipnote.worker; hc
// records who started a terminal in its parent field. See RECIPE.md.
const GROUPS = [
  { status: "blocked", label: "Needs you", color: (t) => t.blocked },
  { status: "error", label: "Failed", color: (t) => t.blocked },
  { status: "working", label: "Working", color: (t) => t.working },
  { status: "done", label: "Done", color: (t) => t.done },
  { status: "idle", label: "Idle", color: (t) => t.idle },
];
const rank = (status) => {
  const i = GROUPS.findIndex((g) => g.status === status);
  return i < 0 ? GROUPS.length : i;
};

const POLL_MS = 5000; // run state only changes when a worker does; poll gently

// buildTree returns the top-level nodes {agent, kids, urgent}: an agent whose
// parent is not itself a listed agent is top level, and its children are the
// agents naming it as parent. urgent is the best rank in the whole subtree.
function buildTree(list, parents) {
  const byAddr = new Set(list.map((a) => a.address));
  const kids = {};
  const top = [];
  for (const a of list) {
    const p = parents[a.address];
    if (p && byAddr.has(p)) (kids[p] ??= []).push(a);
    else top.push(a);
  }
  const node = (a) => {
    const ks = kids[a.address] ?? [];
    return { agent: a, kids: ks, urgent: Math.min(rank(a.status), ...ks.map((k) => rank(k.status))) };
  };
  return top.map(node);
}

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

  icon(agent, t) {
    return agent.program?.icon
      ? (agent.program.tint ? svg(agent.program.icon).text_color(t.foreground) : image(agent.program.icon))
      : div();
  }

  // A delegator or a flat agent: icon, why, who. A click focuses it.
  row(agent, group, labels) {
    const t = theme();
    const why = labels[agent.address]?.["wipnote.title"] ?? agent.tab_name;
    const who = this.runs[agent.address]?.name ?? labels[agent.address]?.["wipnote.worker"] ?? agent.program.name;
    return h_flex().id(`agent-${agent.address}`).w_full().flex_none().gap(8).px(10).py(6).items_center()
      .cursor_pointer().when(agent.focused, (el) => el.bg(t.selected)).hover((s) => s.bg(t.hover))
      .on_click(() => run("agent.focus", { terminal: agent.address }))
      .child(div().w(3).h(30).rounded(2).flex_none().bg(group.color(t)))
      .child(this.icon(agent, t).size(16).flex_none())
      .child(v_flex().flex_1().min_w(0)
        .child(div().text_size(12).text_color(t.foreground).truncate().child(why))
        .child(div().text_size(11).text_color(t.muted).truncate().child(`${who} · ${agent.space_name}`)));
  }

  // A worker: title first line, name second, a bar in its own status colour. Its
  // tab is put away, so a click peeks at the terminal instead of focusing it.
  worker(agent, labels) {
    const t = theme();
    const g = GROUPS[Math.min(rank(agent.status), GROUPS.length - 1)];
    const title = (labels[agent.address]?.["wipnote.title"] ?? agent.title ?? "").split("\n")[0];
    const name = this.runs[agent.address]?.name ?? labels[agent.address]?.["wipnote.worker"] ?? agent.program.name;
    return h_flex().id(`worker-${agent.address}`).w_full().flex_none().gap(8).pl(28).pr(10).py(4).items_center()
      .cursor_pointer().hover((s) => s.bg(t.hover))
      .on_click(() => run("terminal.peek", { terminal: agent.address }))
      .child(div().w(3).h(26).rounded(2).flex_none().bg(g.color(t)))
      .child(v_flex().flex_1().min_w(0)
        .child(div().text_size(12).text_color(t.foreground).truncate().child(title))
        .child(div().text_size(11).text_color(t.muted).truncate().child(name)));
  }

  node(n, group, labels) {
    const t = theme();
    const rows = [this.row(n.agent, group, labels)];
    if (n.kids.length > 0) {
      rows.push(div().pl(28).pt(2).text_size(11).text_color(t.dim).child(`workers · ${n.kids.length}`));
      for (const k of n.kids) rows.push(this.worker(k, labels));
    }
    return v_flex().w_full().flex_none().children(rows);
  }

  render() {
    const t = theme();
    const labels = {};
    const parents = {};
    for (const term of terminals()) {
      labels[term.address] = term.labels ?? {};
      if (term.parent) parents[term.address] = term.parent;
    }
    const tree = buildTree(agents(), parents);
    const groups = GROUPS.map((group, i) => ({ group, members: tree.filter((n) => n.urgent === i) }))
      .filter((g) => g.members.length > 0);
    setAgentOrder(groups.flatMap((g) => g.members.flatMap((n) => [n.agent, ...n.kids].map((a) => a.address))));
    return v_flex().id("wipnote-board").size_full().bg(t.chrome).overflow_y_scrollbar().pb(8)
      .child(div().px(10).pt(10).pb(6).text_size(11).font_semibold().text_color(t.muted).child("WORK"))
      .when(groups.length === 0, (el) => el.child(div().px(10).text_size(12).text_color(t.dim).child("No agents are running.")))
      .children(groups.map(({ group, members }) => v_flex().w_full().flex_none()
        .child(h_flex().px(10).pt(8).pb(2).gap(6).text_size(11).text_color(t.muted)
          .child(div().font_semibold().child(group.label))
          .child(div().text_color(t.dim).child(String(members.length))))
        .children(members.map((n) => this.node(n, group, labels)))));
  }
}
