// The opening hours. Part of the restaurant page.

import type { App } from "../../app";
import { api, errorMessage } from "../../api";
import { el } from "../../dom";
import { formatWeekday } from "../../format";
import { button, field, input, select } from "../../components/forms";
import { dirtyTracker, type EditableSection } from "../../components/editable";
import { actions, card } from "../page";
import type { OpeningPeriod, RestaurantDetail } from "../../types";
import { statusLine } from "../../components/status";

/**
 * The opening hours editor.
 *
 * The whole set is replaced in one PUT, which is what the API offers and what
 * matches how a person edits them: a lunch break is two rows that only make
 * sense together.
 */
export function hoursSection(app: App, restaurant: RestaurantDetail): EditableSection {
  const { t } = app;
  const status = statusLine();
  const list = el("div", { class: "rows" });

  interface Row {
    element: HTMLElement;
    day: HTMLSelectElement;
    start: HTMLInputElement;
    end: HTMLInputElement;
  }
  const rows: Row[] = [];

  const days = [1, 2, 3, 4, 5, 6, 7].map((day) => ({
    value: String(day),
    label: formatWeekday(app.language, day),
  }));

  function addRow(period?: OpeningPeriod): void {
    const day = select(days, { value: String(period?.day_of_week ?? 1) });
    const start = input({ type: "time", value: period?.start ?? "11:00" });
    const end = input({ type: "time", value: period?.end ?? "14:00" });

    // "Crosses midnight" is a hint, not an error: a kebab shop that closes at
    // 02:00 is the normal case, not a mistake (F3.3).
    //
    // Its column is always there, empty or not. A note that appeared and
    // disappeared would push the row's controls sideways as the times were
    // typed, and a list of opening hours would never settle into a shape.
    const hint = el("span", { class: "hint-inline hours-hint" });
    const updateHint = (): void => {
      hint.textContent = end.value < start.value ? t.t("restaurant.crosses_midnight") : "";
    };
    start.addEventListener("input", updateHint);
    end.addEventListener("input", updateHint);
    updateHint();

    const row: Row = {
      day,
      start,
      end,
      element: el(
        "div",
        { class: "row hours-row" },
        field({ label: t.t("restaurant.hours.day"), control: day }),
        field({ label: t.t("restaurant.hours.from"), control: start }),
        field({ label: t.t("restaurant.hours.to"), control: end }),
        hint,
        actions(
          button({
            // Red, like every other remove in the application. It was the one
            // that was not, which made it read as the odd one out rather than
            // as the same action.
            label: t.t("action.remove"),
            variant: "danger",
            onclick: () => {
              const index = rows.indexOf(row);
              if (index >= 0) {
                rows.splice(index, 1);
                row.element.remove();
                // Removing a row is a change: the whole set goes in one PUT.
                touched();
              }
            },
          }),
        ),
      ),
    };

    rows.push(row);
    list.appendChild(row.element);
  }

  for (const period of restaurant.opening_hours) {
    addRow(period);
  }
  if (restaurant.opening_hours.length === 0) {
    list.appendChild(el("p", { class: "muted", text: t.t("restaurant.hours.none") }));
  }

  // What the rows say, as the API wants it. Also the comparison this section
  // uses to decide whether anything has changed.
  const current = (): string =>
    JSON.stringify(
      rows.map((row) => ({
        day_of_week: Number.parseInt(row.day.value, 10),
        start: row.start.value,
        end: row.end.value,
      })),
    );

  const tracker = dirtyTracker(current);
  const touched = tracker.touched;
  // One listener on the list rather than one per control, so rows added later
  // are covered without anything remembering to wire them up.
  list.addEventListener("input", touched);
  list.addEventListener("change", touched);

  async function save(): Promise<void> {
    status.clear();
    try {
      await api.put(`/restaurants/${restaurant.id}/opening-hours`, {
        opening_hours: JSON.parse(current()) as unknown,
      });
      status.say(t.t("state.saved"));
      tracker.saved();
    } catch (error) {
      status.fail(errorMessage(t, error));
      throw error;
    }
  }

  const element = card(
    list,
    actions(
      button({
        label: t.t("restaurant.hours.add"),
        variant: "primary",
        onclick: () => {
          // The "none" message is not a row and must go when one appears.
          list.querySelector(".muted")?.remove();
          addRow();
          // Adding an empty row is a change in itself: the whole set is written
          // in one PUT, so a row nobody has typed into still has to be saved.
          touched();
        },
      }),
    ),
    status.element,
  );

  return {
    element,
    save,
    onDirtyChange: tracker.onDirtyChange,
  };
}
