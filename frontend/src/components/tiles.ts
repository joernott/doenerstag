// The tile grid.
//
// Both overview pages -- orders and restaurants -- are a grid of tiles whose
// first entry is a plus that creates a new one (docs/06_ui_ux.md). The column
// count follows the breakpoints there: one, two, three, four.

import { append, el, icon, type Child } from "../dom";

/** A grid of tiles. */
export function tileGrid(...tiles: Child[]): HTMLElement {
  const grid = el("div", { class: "tile-grid", role: "list" });
  append(grid, ...tiles);
  return grid;
}

export interface TileOptions {
  /** Where the tile leads. */
  href: string;
  /** The tile's heading, which is also the link that covers it. */
  title: string;
  /** An accessible name for the link, when the title alone is not enough. */
  label?: string;
  /**
   * Buttons inside the tile.
   *
   * A tile used to be one big `<a>`, which meant a control inside it had to
   * live underneath in a footer -- a link inside a link is not something a
   * browser can make sense of. The tile is now a card whose *title* is the
   * link, stretched over the card by `.tile-link::after`, so the whole card is
   * still clickable and real buttons can sit on top of it. That is what lets an
   * order tile carry a summary, an edit and a delete.
   */
  actions?: HTMLElement;
  /**
   * A picture above the title: a restaurant's logo, or ours standing in for it.
   *
   * Its own option rather than the first child, because the title has to be the
   * first thing in the card for the stretched link to sit under everything, and
   * the picture has to be above the title on the screen.
   */
  media?: HTMLElement;
  /**
   * Faded, for an expired order.
   *
   * The fading is never the only signal: the caller also puts the state in the
   * tile as text, because a colour is not information.
   */
  faded?: boolean;
}

/** One tile. */
export function tile(options: TileOptions, ...children: Child[]): HTMLElement {
  const card = el("div", { class: `tile ${options.faded ? "tile-faded" : ""}`.trim() });

  if (options.media) {
    card.appendChild(options.media);
  }
  card.appendChild(
    el("a", {
      class: "tile-link",
      href: options.href,
      text: options.title,
      ...(options.label ? { "aria-label": options.label } : {}),
    }),
  );
  append(card, ...children);
  if (options.actions) {
    card.appendChild(options.actions);
  }

  return el("div", { role: "listitem", class: "tile-cell" }, card);
}

/** The row of buttons at the foot of a tile. */
export function tileActions(...content: Child[]): HTMLElement {
  const row = el("div", { class: "tile-actions" });
  append(row, ...content);
  return row;
}

/**
 * The plus tile.
 *
 * Always first, and shown even to an anonymous visitor, for whom it leads to
 * the login page rather than being hidden -- the empty state and the "you could
 * join in" state are the same picture, which is the point.
 *
 * The plus is the tile: it is sized in CSS to about three quarters of the
 * card, so the thing you click to start is the most obvious thing on the page.
 */
export function addTile(href: string, label: string): HTMLElement {
  const card = el(
    "div",
    { class: "tile tile-add-card" },
    el("div", { class: "tile-add" }, icon("plus")),
    el("a", { class: "tile-link tile-add-label", href, text: label }),
  );
  return el("div", { role: "listitem", class: "tile-cell" }, card);
}
