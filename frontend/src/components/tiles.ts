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
  /** The tile's accessible name, when the visible content is not enough. */
  label?: string;
  /**
   * Faded, for an expired order.
   *
   * The fading is never the only signal: the caller also puts the state in the
   * tile as text, because opacity is not information.
   */
  faded?: boolean;
}

/** One tile. */
export function tile(options: TileOptions, ...children: Child[]): HTMLElement {
  const link = el("a", {
    class: `tile ${options.faded ? "tile-faded" : ""}`.trim(),
    href: options.href,
    ...(options.label ? { "aria-label": options.label } : {}),
  });
  append(link, ...children);
  return el("div", { role: "listitem", class: "tile-cell" }, link);
}

/**
 * The plus tile.
 *
 * Always first, and shown even to an anonymous visitor, for whom it leads to
 * the login page rather than being hidden -- the empty state and the "you could
 * join in" state are the same picture, which is the point.
 */
export function addTile(href: string, label: string): HTMLElement {
  return tile(
    { href, label },
    el("div", { class: "tile-add" }, icon("plus")),
    el("span", { class: "tile-add-label", text: label }),
  );
}
