// Tabs, for a page whose sections are each worth a click of their own: the
// restaurant page, where the menu -- the part somebody is nearly always there
// for -- would otherwise sit below the contacts and the opening hours, and the
// account page.
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
  /**
   * A tab that cannot be selected, and why: the order page's Chat for
   * somebody not taking part in the order. Shown rather than left out, like
   * the summary link, so the feature is visibly there and says what it needs.
   */
  disabled?: string;
}

export interface TabsOptions {
  /** Which tab starts selected. Out-of-range falls back to the first. */
  initial?: number;
  /** The tablist's accessible name. */
  label: string;
}

/** A tab strip whose tabs can change after it is built. */
export interface TabControls {
  readonly element: HTMLElement;
  /** Renames a tab: the order page's "Chat (3)" counts as messages arrive. */
  setLabel(id: string, label: string): void;
  /**
   * Makes a tab selectable, or not and why. A selected tab that becomes
   * unavailable hands the selection to the first tab, so a panel nobody may
   * see is never left open.
   */
  setDisabled(id: string, reason: string | undefined): void;
  /** Whether a tab is the one showing. */
  isSelected(id: string): boolean;
}

/** Builds a tab strip and its panels. */
export function tabs(specs: TabSpec[], options: TabsOptions): HTMLElement {
  return tabControls(specs, options).element;
}

/** Builds a tab strip and its panels, and the means to change them later. */
export function tabControls(specs: TabSpec[], options: TabsOptions): TabControls {
  // Every tab strip in the application is built here, so there is one tab look
  // rather than two that drift apart.
  const strip = el("div", { class: "tabs", role: "tablist", "aria-label": options.label });
  const panels = el("div", { class: "tab-panels" });

  const buttons: HTMLButtonElement[] = [];
  let current = options.initial !== undefined && specs[options.initial] ? options.initial : 0;

  const disabled = specs.map((spec) => spec.disabled);

  const select = (next: number, moveFocus: boolean): void => {
    if (disabled[next] !== undefined) {
      return;
    }
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
        // The arrow keys step over a tab that cannot be chosen, the way they
        // would if it were not there.
        const step = (from: number, by: 1 | -1): number => {
          let next = from;
          do {
            next = (next + by + specs.length) % specs.length;
          } while (disabled[next] !== undefined && next !== from);
          return next;
        };
        const enabled = specs.map((_, i) => i).filter((i) => disabled[i] === undefined);
        const moves: Record<string, number | undefined> = {
          ArrowRight: step(index, 1),
          ArrowLeft: step(index, -1),
          Home: enabled[0],
          End: enabled[enabled.length - 1],
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

  const markDisabled = (index: number): void => {
    const button = buttons[index];
    const reason = disabled[index];
    if (!button) {
      return;
    }
    // aria-disabled rather than disabled: the reason is the tab's title, and a
    // disabled button shows no title in some browsers.
    if (reason === undefined) {
      button.removeAttribute("aria-disabled");
      button.removeAttribute("title");
    } else {
      button.setAttribute("aria-disabled", "true");
      button.title = reason;
    }
  };

  specs.forEach((_, index) => {
    markDisabled(index);
  });
  if (disabled[current] !== undefined) {
    current = disabled.findIndex((reason) => reason === undefined);
  }
  select(current, false);

  const indexOf = (id: string): number => specs.findIndex((spec) => spec.id === id);

  return {
    element: el("div", { class: "tab-section" }, strip, panels),
    setLabel(id: string, label: string): void {
      const button = buttons[indexOf(id)];
      if (button && button.textContent !== label) {
        button.textContent = label;
      }
    },
    setDisabled(id: string, reason: string | undefined): void {
      const index = indexOf(id);
      if (index < 0 || disabled[index] === reason) {
        return;
      }
      disabled[index] = reason;
      markDisabled(index);
      if (reason !== undefined && index === current) {
        select(disabled.findIndex((other) => other === undefined), false);
      }
    },
    isSelected(id: string): boolean {
      return indexOf(id) === current;
    },
  };
}
