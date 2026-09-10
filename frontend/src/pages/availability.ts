// When food can be had: the filters, and the controls that attach them.
//
// A filter is a named rule -- "Mittagsmenü", "Fri-Sun after 5" -- defined once
// for a restaurant and attached to as many categories and items as it applies
// to. Its own parts are ANDed; several filters on one element are alternatives;
// a category's and an item's must both hold. The server decides all of that
// (docs/03_data_model.md), so this file is about saying it clearly, not about
// implementing it a second time.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el, icon, type Child } from "../dom";
import { formatWeekday } from "../format";
import { button, field, form, input } from "../components/forms";
import { confirmDialog, openModal } from "../components/modal";
import { statusLine } from "./page";
import type { AvailabilityFilter } from "../components/menuitem";

/** ISO weekdays, Monday first, which is how the schema numbers them. */
const WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const;

/** Loads a restaurant's filters. */
export function loadFilters(restaurantID: string): Promise<AvailabilityFilter[]> {
  return getList<AvailabilityFilter>(
    `/restaurants/${restaurantID}/availability`,
    "availability",
  ).catch(() => []);
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

/**
 * The Availability tab: define the rules this restaurant's menu can refer to.
 *
 * Deleting is the administrator's, as with every other piece of menu structure,
 * because a filter is shared by everything it is attached to.
 */
export function availabilitySection(app: App, restaurantID: string): HTMLElement {
  const { t } = app;
  const status = statusLine();
  const list = el("div", { class: "rows" });
  let filters: AvailabilityFilter[] = [];

  const section = el(
    "div",
    { class: "stack" },
    el("p", { class: "field-hint", text: t.t("availability.explain") }),
    list,
    el(
      "div",
      { class: "row" },
      button({
        label: t.t("availability.new"),
        onclick: () => {
          openFilterEditor(app, restaurantID, null, reload);
        },
      }),
    ),
    status.element,
  );

  function render(): void {
    if (filters.length === 0) {
      list.replaceChildren(el("p", { class: "muted", text: t.t("availability.none") }));
      return;
    }
    list.replaceChildren(...filters.map((filter) => row(filter)));
  }

  function row(filter: AvailabilityFilter): HTMLElement {
    const controls: Child[] = [
      button({
        label: t.t("action.edit"),
        variant: "quiet",
        ariaLabel: `${t.t("action.edit")}: ${filter.name}`,
        onclick: () => {
          openFilterEditor(app, restaurantID, filter, reload);
        },
      }),
    ];

    if (app.session.isAdmin) {
      controls.push(
        button({
          label: t.t("action.delete"),
          variant: "danger",
          ariaLabel: `${t.t("action.delete")}: ${filter.name}`,
          onclick: () => {
            void remove(filter);
          },
        }),
      );
    }

    return el(
      "div",
      { class: "availability-row" },
      el(
        "div",
        { class: "availability-text" },
        el("strong", { text: filter.name }),
        el("span", { class: "muted", text: describeFilter(app, filter) }),
      ),
      el("div", { class: "row" }, ...controls),
    );
  }

  async function remove(filter: AvailabilityFilter): Promise<void> {
    const confirmed = await confirmDialog({
      t,
      title: t.t("availability.delete_title"),
      // Said plainly: a filter is shared, so removing it changes every menu
      // element it was attached to at once.
      message: t.t("availability.delete_warning", { name: filter.name }),
      confirmLabel: t.t("action.delete"),
      danger: true,
    });
    if (!confirmed) {
      return;
    }

    status.clear();
    try {
      await api.delete(`/restaurants/${restaurantID}/availability/${filter.id}`);
      await reload();
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  async function reload(): Promise<void> {
    filters = await loadFilters(restaurantID);
    render();
  }

  void reload();
  return section;
}

/** The dialog that defines or changes one filter. */
export function openFilterEditor(
  app: App,
  restaurantID: string,
  existing: AvailabilityFilter | null,
  onSaved: () => void | Promise<void>,
): void {
  const { t } = app;
  const editorStatus = statusLine();

  const name = input({ value: existing?.name ?? "", required: true });
  const onDate = input({ type: "date", value: existing?.on_date ?? "" });
  const startTime = input({ type: "time", value: existing?.start_time ?? "" });
  const endTime = input({ type: "time", value: existing?.end_time ?? "" });

  const days = new Map<number, HTMLInputElement>();
  const dayRow = el("div", { class: "weekday-row" });
  for (const day of WEEKDAYS) {
    const box = el("input", {
      type: "checkbox",
      class: "checkbox",
      checked: existing?.weekdays.includes(day) ?? false,
    });
    days.set(day, box);
    dayRow.appendChild(
      el("label", { class: "checkbox-row" }, box, el("span", { text: formatWeekday(app.language, day) })),
    );
  }

  const save = button({ label: t.t("action.save"), variant: "primary", type: "submit" });

  const body = form(
    () => {
      void store();
    },
    field({ label: t.t("availability.name"), control: name }),
    el("p", { class: "field-hint", text: t.t("availability.name_hint") }),
    field({
      label: t.t("availability.on_date"),
      control: onDate,
      optionalLabel: t.t("auth.optional"),
    }),
    el("fieldset", { class: "fieldset" }, el("legend", { text: t.t("availability.weekdays") }), dayRow),
    el(
      "div",
      { class: "row" },
      field({
        label: t.t("availability.from"),
        control: startTime,
        optionalLabel: t.t("auth.optional"),
      }),
      field({
        label: t.t("availability.until"),
        control: endTime,
        optionalLabel: t.t("auth.optional"),
      }),
    ),
    el("p", { class: "field-hint", text: t.t("availability.time_hint") }),
    editorStatus.element,
  );

  async function store(): Promise<void> {
    editorStatus.clear();

    const weekdays = [...days.entries()]
      .filter(([, box]) => box.checked)
      .map(([day]) => day);

    const payload = {
      name: name.value.trim(),
      on_date: onDate.value === "" ? null : onDate.value,
      weekdays,
      start_time: startTime.value === "" ? null : startTime.value,
      end_time: endTime.value === "" ? null : endTime.value,
    };

    save.disabled = true;
    try {
      if (existing) {
        await api.patch(`/restaurants/${restaurantID}/availability/${existing.id}`, payload);
      } else {
        await api.post(`/restaurants/${restaurantID}/availability`, payload);
      }
      modal.close();
      await onSaved();
    } catch (error) {
      editorStatus.fail(errorMessage(t, error));
    } finally {
      save.disabled = false;
    }
  }

  const modal = openModal({
    title: existing ? t.t("availability.edit_title") : t.t("availability.new"),
    body,
    actions: [save],
    closeLabel: t.t("action.close"),
  });
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

export interface AttachmentEditorOptions {
  app: App;
  restaurantID: string;
  kind: "categories" | "menu-items";
  elementID: string;
  /** What is being edited, for the dialog's heading. */
  title: string;
  attached: readonly AvailabilityFilter[];
  onSaved: () => void | Promise<void>;
}

/**
 * The dialog that says when one category or one dish is available.
 *
 * It loads the restaurant's filters when it opens rather than taking them as an
 * argument: the list changes on the Availability tab, and a dialog holding a
 * copy from page load would offer a rule somebody has since deleted.
 */
export function openAttachmentEditor(options: AttachmentEditorOptions): void {
  const { app, restaurantID, kind, elementID, title, attached, onSaved } = options;
  const { t } = app;

  const editorStatus = statusLine();
  const holder = el("div", { class: "stack" }, el("p", { class: "muted", text: t.t("app.loading") }));
  const save = button({
    label: t.t("action.save"),
    variant: "primary",
    type: "submit",
    disabled: true,
  });

  let read: () => string[] = () => [];

  const body = form(
    () => {
      void store();
    },
    holder,
    editorStatus.element,
  );

  void loadFilters(restaurantID).then((filters) => {
    const picker = filterPicker(app, filters, attached.map((filter) => filter.id));
    read = picker.selected;
    holder.replaceChildren(picker.element);
    save.disabled = false;
  });

  async function store(): Promise<void> {
    editorStatus.clear();
    save.disabled = true;
    try {
      await saveAttachments(restaurantID, kind, elementID, read());
      modal.close();
      await onSaved();
    } catch (error) {
      editorStatus.fail(errorMessage(t, error));
    } finally {
      save.disabled = false;
    }
  }

  const modal = openModal({
    title: `${t.t("availability.attached")}: ${title}`,
    body,
    actions: [save],
    closeLabel: t.t("action.close"),
  });
}
