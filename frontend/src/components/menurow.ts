// A dish as a menu shows it: its picture, its number, its name, whether it is
// sold out, what it is and what it contains. The restaurant page's menu and the
// order page's menu column both draw it, with only the end of the row
// differing between them -- Edit on one, the price and Add on the other.

import type { App } from "../app";
import { el } from "../dom";
import { referenceName, tagName } from "../i18n";
import type { MenuItem } from "../types";
import { thumbnailURL } from "./images";
import { availabilityChip } from "./availability";

/** The marks an item carries: its tags, allergens and additives, as chips. */
export function itemChips(app: App, item: MenuItem): HTMLElement[] {
  const { t } = app;

  // A dish with a rule says so, and says which rule, so that somebody reading
  // the menu can tell "we are out of it today" from "we make it at weekends".
  const when = availabilityChip(app, item.availability ?? []);

  return [
    ...(when === null ? [] : [when]),
    ...item.tags.map((tag) => chip(tagName(t, tag), "chip-tag")),
    ...item.allergens.map((allergen) =>
      chip(referenceName(t, "allergen", allergen.code), "chip-allergen"),
    ),
    ...item.additives.map((additive) =>
      chip(referenceName(t, "additive", additive.code), "chip-additive"),
    ),
  ];
}

function chip(text: string, className: string): HTMLElement {
  return el("span", { class: `chip ${className}`, text });
}


/**
 * The parts of a menu row every menu shows: the thumbnail, and the text with
 * its marks. The page adds what it lets somebody do with the dish.
 */
export function menuItemParts(app: App, item: MenuItem): HTMLElement[] {
  const { t } = app;
  const marks = itemChips(app, item);
  return [
    item.image_id
      ? el("img", { class: "menu-thumb", src: thumbnailURL(item.image_id), alt: "", loading: "lazy" })
      : el("div", { class: "menu-thumb menu-thumb-empty", "aria-hidden": "true" }),
    el(
      "div",
      { class: "menu-item-text" },
      el(
        "div",
        { class: "menu-item-head" },
        item.external_id ? el("span", { class: "menu-number", text: item.external_id }) : null,
        el("strong", { text: item.name }),
        // Never colour or a strike-through alone: the state is also a word.
        item.available ? null : el("span", { class: "badge", text: t.t("menu.item.unavailable") }),
      ),
      item.description ? el("p", { class: "muted", text: item.description }) : null,
      marks.length > 0 ? el("div", { class: "chips" }, ...marks) : null,
    ),
  ];
}
