// A list of labels and values: the facts about an order, its totals, the
// version page. The same dl/dt/dd loop was written out on every page that has
// one (20.28).

import { append, el, type Child } from "../dom";

/** One row: the label, and the value, which may be text or an element. */
export type Definition = [label: string, value: Child];

export function definitionList(rows: Definition[], className = "definitions"): HTMLElement {
  const list = el("dl", { class: className });
  for (const [label, value] of rows) {
    list.appendChild(el("dt", { text: label }));
    const dd = el("dd");
    append(dd, value);
    list.appendChild(dd);
  }
  return list;
}
