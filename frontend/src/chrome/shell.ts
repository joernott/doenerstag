// The page shell: everything that is on the screen whatever page is open.

import type { App } from "../app";
import { el } from "../dom";
import { renderTitleBar } from "./titlebar";

export interface Shell {
  /** The whole shell, ready to be put on the page. */
  element: HTMLElement;
  /** Where the router renders pages. */
  outlet: HTMLElement;
  /** The polite live region, for changes that arrive without being asked for. */
  liveRegion: HTMLElement;
}

/** Builds the shell for the current language, theme and session. */
export function renderShell(app: App): Shell {
  const { t } = app;

  // The first focusable element on the page, and visible only when focused:
  // a keyboard user should not have to tab through the whole title bar to
  // reach the order they came for.
  const skip = el("a", { class: "skip-link", href: "#main", text: t.t("nav.skip") });

  const outlet = el("main", {
    class: "page",
    id: "main",
    // Not a tab stop, but focusable, so the router can move focus here after a
    // navigation without adding one.
    tabindex: "-1",
  });

  const liveRegion = el("div", {
    class: "visually-hidden",
    id: "live-region",
    role: "status",
    "aria-live": "polite",
  });

  const element = el(
    "div",
    { class: "shell", id: "app-root" },
    skip,
    renderTitleBar(app),
    outlet,
    liveRegion,
  );

  return { element, outlet, liveRegion };
}
