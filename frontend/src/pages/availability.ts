// When food can be had: the filters, and the controls that attach them.
//
// A filter is a named rule -- "Mittagsmenü", "Fri-Sun after 5" -- defined once
// for a restaurant and attached to as many categories and items as it applies
// to. Its own parts are ANDed; several filters on one element are alternatives;
// a category's and an item's must both hold. The server decides all of that
// (docs/03_data_model.md), so this file is about saying it clearly, not about
// implementing it a second time.

import type { App } from "../app";
import { api } from "../api";
import { el, type Child } from "../dom";
import { formatWeekday } from "../format";
import { button, field, input } from "../components/forms";
import { describeFilter, filterPicker, loadFilters, saveAttachments } from "../components/availability";
import { dialogHint, openFormDialog } from "../components/formdialog";
import { confirmDialog } from "../components/modal";
import type { AvailabilityFilter } from "../types";
import { runAction, statusLine } from "../components/status";

/** ISO weekdays, Monday first, which is how the schema numbers them. */
const WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const;

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
          onclick: () => remove(filter),
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

    await runAction({ status, t }, async () => {
      await api.delete(`/restaurants/${restaurantID}/availability/${filter.id}`);
      await reload();
    });
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

  openFormDialog({
    t,
    title: existing ? t.t("availability.edit_title") : t.t("availability.new"),
    focus: name,
    fields: [
      field({ label: t.t("availability.name"), control: name }),
      dialogHint(t.t("availability.name_hint")),
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
      dialogHint(t.t("availability.time_hint")),
    ],
    onSubmit: async () => {
      const payload = {
        name: name.value.trim(),
        on_date: onDate.value === "" ? null : onDate.value,
        weekdays: [...days.entries()].filter(([, box]) => box.checked).map(([day]) => day),
        start_time: startTime.value === "" ? null : startTime.value,
        end_time: endTime.value === "" ? null : endTime.value,
      };
      if (existing) {
        await api.patch(`/restaurants/${restaurantID}/availability/${existing.id}`, payload);
      } else {
        await api.post(`/restaurants/${restaurantID}/availability`, payload);
      }
    },
    after: onSaved,
  });
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

  const holder = el("div", { class: "stack" }, el("p", { class: "muted", text: t.t("app.loading") }));
  let read: () => string[] = () => [];

  const dialog = openFormDialog({
    t,
    title: `${t.t("availability.attached")}: ${title}`,
    fields: [holder],
    onSubmit: () => saveAttachments(restaurantID, kind, elementID, read()),
    after: onSaved,
  });

  // Nothing to save until the rules have arrived.
  dialog.submit.disabled = true;
  void loadFilters(restaurantID).then((filters) => {
    const picker = filterPicker(app, filters, attached.map((filter) => filter.id));
    read = picker.selected;
    holder.replaceChildren(picker.element);
    dialog.submit.disabled = false;
  });
}
