// Tabs.
//
// The restaurant page was four cards stacked in a column, which meant scrolling
// past the contacts and the opening hours to reach the menu -- the part
// somebody is nearly always there for. The same four sections as tabs put every
// one of them a single click away and make the menu the landing place.
//
// The pattern is the ARIA tabs one: a `tablist` of `tab` buttons, each
// controlling a `tabpanel`. Roving tabindex, so the whole strip is one stop in
// the tab order and the arrow keys move within it, which is what a screen
// reader user expects a tablist to do. Selection follows the arrow keys
// (automatic activation): the panels are already built, so there is nothing to
// wait for and nothing to be gained by making somebody press Enter as well.

import { el } from "../dom";

export interface TabSpec {
  /** Stable within the page; used to build the element ids. */
  id: string;
  label: string;
  panel: HTMLElement;
}

export interface TabsOptions {
  /** Which tab starts selected. Out-of-range falls back to the first. */
  initial?: number;
  /** The tablist's accessible name. */
  label: string;
}

/** Builds a tab strip and its panels. */
export function tabs(specs: TabSpec[], options: TabsOptions): HTMLElement {
  // The classes are the ones the account page already uses for its own two
  // tabs, so there is one tab look in the application rather than two that
  // drift apart. That page still builds its strip by hand; folding it into this
  // component is worth doing and is not this change.
  const strip = el("div", { class: "tabs", role: "tablist", "aria-label": options.label });
  const panels = el("div", { class: "tab-panels" });

  const buttons: HTMLButtonElement[] = [];
  let current = options.initial !== undefined && specs[options.initial] ? options.initial : 0;

  const select = (next: number, moveFocus: boolean): void => {
    current = next;
    specs.forEach((spec, index) => {
      const chosen = index === next;
      const button = buttons[index];
      if (!button) {
        return;
      }
      button.setAttribute("aria-selected", String(chosen));
      // Roving tabindex: only the selected tab is a tab stop, so Tab leaves the
      // strip rather than walking through every tab in it.
      button.tabIndex = chosen ? 0 : -1;
      button.classList.toggle("tab-selected", chosen);
      spec.panel.hidden = !chosen;
    });
    if (moveFocus) {
      buttons[next]?.focus();
    }
  };

  specs.forEach((spec, index) => {
    const tabId = `tab-${spec.id}`;
    const panelId = `panel-${spec.id}`;

    const button = el("button", {
      type: "button",
      class: "tab",
      role: "tab",
      id: tabId,
      "aria-controls": panelId,
      text: spec.label,
      onclick: () => select(index, false),
      onkeydown: (raw: Event) => {
        const event = raw as KeyboardEvent;
        const last = specs.length - 1;
        const moves: Record<string, number> = {
          ArrowRight: index === last ? 0 : index + 1,
          ArrowLeft: index === 0 ? last : index - 1,
          Home: 0,
          End: last,
        };
        const next = moves[event.key];
        if (next === undefined) {
          return;
        }
        event.preventDefault();
        select(next, true);
      },
    });

    spec.panel.classList.add("panel");
    spec.panel.setAttribute("role", "tabpanel");
    spec.panel.setAttribute("id", panelId);
    spec.panel.setAttribute("aria-labelledby", tabId);
    // A panel has to be reachable by keyboard: its content may be text with no
    // focusable control in it, and a panel nobody can reach is a panel nobody
    // can read.
    spec.panel.tabIndex = 0;

    buttons.push(button);
    strip.appendChild(button);
    panels.appendChild(spec.panel);
  });

  select(current, false);
  return el("div", { class: "tab-section" }, strip, panels);
}
