// Choosing which availability rules apply to a category or a dish, wherever
// that is asked: the restaurant page's attachment dialog and the menu item
// editor. A component rather than part of a page, so the editor does not have
// to import a page.

import { api, getList } from "../api";
import { el, icon } from "../dom";
import type { App } from "../app";
import { formatWeekday } from "../format";
import type { AvailabilityFilter } from "../types";

/** Loads a restaurant's filters. */
export function loadFilters(restaurantID: string): Promise<AvailabilityFilter[]> {
  return getList<AvailabilityFilter>(
    `/restaurants/${restaurantID}/availability`,
    "availability",
  ).catch(() => []);
}

/**
 * The control that says which filters an element has.
 *
 * A set of checkboxes rather than a search: a restaurant has a handful of these
 * and the point of the panel is to see at a glance what is ticked. It returns
 * the element and a reader, so the form that owns it decides when to save.
 */
export function filterPicker(
  app: App,
  filters: readonly AvailabilityFilter[],
  attached: readonly string[],
): { element: HTMLElement; selected: () => string[] } {
  const { t } = app;

  if (filters.length === 0) {
    return {
      element: el("p", { class: "field-hint", text: t.t("availability.none_defined") }),
      selected: () => [],
    };
  }

  const boxes = new Map<string, HTMLInputElement>();
  const rows = filters.map((filter) => {
    const box = el("input", {
      type: "checkbox",
      class: "checkbox",
      checked: attached.includes(filter.id),
    });
    boxes.set(filter.id, box);
    return el(
      "label",
      { class: "checkbox-row" },
      box,
      el(
        "span",
        {},
        el("span", { text: filter.name }),
        el("span", { class: "muted", text: ` ${describeFilter(app, filter)}` }),
      ),
    );
  });

  return {
    element: el(
      "fieldset",
      { class: "fieldset" },
      el("legend", { text: t.t("availability.attached") }),
      el("p", { class: "field-hint", text: t.t("availability.attach_hint") }),
      ...rows,
    ),
    selected: () =>
      [...boxes.entries()].filter(([, box]) => box.checked).map(([id]) => id),
  };
}

/** Writes an element's attached filters. */
export function saveAttachments(
  restaurantID: string,
  kind: "categories" | "menu-items",
  elementID: string,
  filterIDs: string[],
): Promise<unknown> {
  return api.put(`/restaurants/${restaurantID}/${kind}/${elementID}/availability`, {
    filter_ids: filterIDs,
  });
}

/**
 * A filter in one line: what it is called, and what it actually says.
 *
 * The second half matters more than it looks. A list of names is a list of
 * things somebody has to remember the meaning of, and "Fri-Sun after 5" is only
 * a promise that the rule matches its name.
 */
export function describeFilter(app: App, filter: AvailabilityFilter): string {
  const { t } = app;
  const parts: string[] = [];

  if (filter.on_date) {
    parts.push(filter.on_date);
  }
  if (filter.weekdays.length > 0) {
    parts.push(
      filter.weekdays.map((day) => formatWeekday(app.language, day).slice(0, 2)).join(", "),
    );
  }
  if (filter.start_time && filter.end_time) {
    parts.push(`${filter.start_time}–${filter.end_time}`);
  }

  return parts.length > 0 ? parts.join(" · ") : t.t("availability.always");
}


/** A short marker for a menu row: this dish is not always available. */
export function availabilityChip(
  app: App,
  filters: readonly AvailabilityFilter[],
): HTMLElement | null {
  if (filters.length === 0) {
    return null;
  }
  const { t } = app;
  const summary = filters.map((filter) => filter.name).join(", ");
  return el(
    "span",
    { class: "chip availability-chip", title: `${t.t("availability.attached")}: ${summary}` },
    icon("clock"),
    el("span", { text: summary }),
  );
}

