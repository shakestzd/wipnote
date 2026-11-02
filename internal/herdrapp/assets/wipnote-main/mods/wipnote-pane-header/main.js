// A pane's header for the wipnote-main layout. A worker shows its name and
// the work item it carries; any other pane shows what Herdr's own header does.
// Props from the app: { pane, terminal }. Click: focus the pane. No buttons:
// closing a worker's pane would kill it. See RECIPE.md.

import { View, div, svg, image } from "gpui-kit";
import { h_flex } from "gpui-base";
import { panes, props, run, terminals, theme, home } from "herdr";

function icon(p, size, color) {
  if (!p?.icon) return null;
  return p.tint ? svg(p.icon).size(size).flex_none().text_color(color) : image(p.icon).w(size).h(size).flex_none();
}

function folder(cwd) {
  if (!cwd) return "";
  const h = home();
  return cwd === h ? "~" : cwd.startsWith(h + "/") ? "~" + cwd.slice(h.length) : cwd;
}

export default class WipnotePaneHeader extends View {
  me() {
    return panes().find((p) => p.pane === props()?.pane);
  }

  // The pane's terminal labels. A mod pane has no terminal: no labels.
  labels(p) {
    const addr = props()?.terminal ?? p.terminal;
    if (!addr) return {};
    return terminals().find((x) => x.address === addr)?.labels ?? {};
  }

  status(t, st) {
    if (st === "working") return svg("mods/workspace-chrome/spinner.svg").size(12).flex_none().text_color(t.working).spin(900);
    if (st === "blocked") return div().flex_none().px(6).h(18).flex().items_center().rounded(5).bg(t.blocked + "26").text_size(10.5).font_semibold().text_color(t.blocked).child("Needs you");
    if (st === "done") return div().size(7).flex_none().rounded(4).bg(t.done);
    return null;
  }

  render() {
    const t = theme();
    const p = this.me();
    if (!p) return div();
    const labels = this.labels(p);
    const worker = labels["wipnote.worker"];
    const item = labels["wipnote.title"];
    const fallback = ((p.program?.agent || p.program?.shell) && p.program.name) || p.title || p.program?.name || "shell";
    const pic = icon(p.program, 14, t.dim);
    const pane = p.pane;
    const badge = this.status(t, p.status);
    return h_flex()
      .id(`wn-pane-head-${pane}`)
      .size_full()
      .px(10)
      .gap(8)
      .items_center()
      .bg(t.background)
      .border_b(1)
      .border_color(t.border)
      .text_size(12)
      .on_click(() => run("pane.focus", { pane }))
      .when(Boolean(pic), (el) => el.child(pic))
      .child(div().flex_none().truncate().max_w(260).font_medium().text_color(t.foreground).child(worker || fallback))
      .child(div().flex_1().min_w(0).truncate().text_color(t.dim).child(worker ? item || "" : folder(p.cwd)))
      .when(Boolean(badge), (el) => el.child(badge));
  }
}
