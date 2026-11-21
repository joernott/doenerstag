// Starting a vote (F12.1, docs/06 "Creating a vote").
//
// Two to five restaurants, the decision time, the order deadline and the
// fulfilment time -- in the order they must come, which is the order they are
// on the page -- and the two switches. There is no fulfilment type: the order
// the vote becomes starts as a pickup (F12.9), and the page says so.

import type { App } from "../app";
import { api, getList } from "../api";
import { el } from "../dom";
import { button, checkbox, field, fieldError, form, input } from "../components/forms";
import { localInputValue } from "../format";
import { isOpen } from "../openinghours";
import { runAction, statusLine } from "../components/status";
import { actions, failedPage, page, section } from "./page";
import { timesProblem } from "./vote";
import type { Restaurant, RestaurantDetail } from "../types";

const MAX_CHOICES = 5;
const MIN_CHOICES = 2;

function inHours(hours: number): Date {
  return new Date(Date.now() + hours * 60 * 60 * 1000);
}

export async function createVotePage(app: App): Promise<HTMLElement> {
  const { t } = app;

  let restaurants: Restaurant[];
  try {
    restaurants = await getList<Restaurant>("/restaurants", "restaurants");
  } catch (error) {
    return failedPage(t.t("vote.new"), t, error);
  }
  restaurants.sort((a, b) => a.name.localeCompare(b.name, app.language));

  if (restaurants.length < MIN_CHOICES) {
    return page(
      t.t("vote.new"),
      el("p", { class: "muted", text: t.t("error.1017") }),
      el("p", {}, el("a", { class: "link", href: "/restaurants", text: t.t("restaurant.new") })),
    );
  }

  const status = statusLine();
  const counter = el("span", { class: "muted", "aria-live": "polite" });
  const boxes = restaurants.map((restaurant) =>
    checkbox(restaurant.name, { name: "restaurant_ids", key: `restaurant-${restaurant.id}`, onchange: () => update() }),
  );
  const inputs = boxes.map((box) => box.querySelector("input") as HTMLInputElement);
  inputs.forEach((box, index) => {
    box.value = restaurants[index]?.id ?? "";
  });

  const list = el("div", { class: "vote-pick", role: "group" }, ...boxes);
  const listProblem = fieldError(list);

  const decisionAt = input({ type: "datetime-local", name: "decision_at", value: localInputValue(inHours(1)), required: true });
  const deadlineAt = input({ type: "datetime-local", name: "deadline_at", value: localInputValue(inHours(2)), required: true });
  const fulfilmentAt = input({ type: "datetime-local", name: "fulfilment_at", value: localInputValue(inHours(3)), required: true });
  const timesError = fieldError(decisionAt);
  const hoursWarning = el("p", { class: "field-hint" });

  const openChoices = checkbox(t.t("vote.open_choices"), { name: "open_choices" });
  const publicBallots = checkbox(t.t("vote.public_ballots"), { name: "public_ballots" });

  const hours = new Map<string, RestaurantDetail["opening_hours"]>();

  function chosen(): string[] {
    return inputs.filter((box) => box.checked).map((box) => box.value);
  }

  function update(): void {
    const count = chosen().length;
    counter.textContent = t.t("vote.restaurants.chosen", { count });
    // The sixth cannot be ticked while five are.
    for (const box of inputs) {
      box.disabled = !box.checked && count >= MAX_CHOICES;
    }
    if (count >= MIN_CHOICES) {
      listProblem.clear();
    }
    void warnAboutHours();
  }

  async function warnAboutHours(): Promise<void> {
    const at = new Date(fulfilmentAt.value);
    if (Number.isNaN(at.getTime())) {
      hoursWarning.textContent = "";
      return;
    }
    const closed: string[] = [];
    for (const id of chosen()) {
      if (!hours.has(id)) {
        try {
          hours.set(id, (await api.get<RestaurantDetail>(`/restaurants/${id}`)).opening_hours);
        } catch {
          hours.set(id, []);
        }
      }
      const periods = hours.get(id) ?? [];
      if (periods.length > 0 && !isOpen(periods, at)) {
        closed.push(restaurants.find((restaurant) => restaurant.id === id)?.name ?? "");
      }
    }
    // F5.4: a warning, never a refusal.
    hoursWarning.textContent = closed.length > 0 ? t.t("vote.outside_hours", { names: closed.join(", ") }) : "";
  }

  for (const control of [decisionAt, deadlineAt, fulfilmentAt]) {
    control.addEventListener("input", () => {
      const message = timesProblem(app, decisionAt.value, deadlineAt.value, fulfilmentAt.value);
      if (message) {
        timesError.set(message);
      } else {
        timesError.clear();
      }
      void warnAboutHours();
    });
  }

  const submit = button({ label: t.t("vote.new"), variant: "primary", type: "submit" });

  async function create(): Promise<void> {
    status.clear();
    const ids = chosen();
    if (ids.length < MIN_CHOICES) {
      listProblem.set(t.t("error.1017"));
      inputs[0]?.focus();
      return;
    }
    const message = timesProblem(app, decisionAt.value, deadlineAt.value, fulfilmentAt.value);
    if (message) {
      timesError.set(message);
      decisionAt.focus();
      return;
    }

    await runAction({ status, t, busy: submit }, async () => {
      const created = await api.post<{ id: string }>("/votes", {
        restaurant_ids: ids,
        decision_at: new Date(decisionAt.value).toISOString(),
        deadline_at: new Date(deadlineAt.value).toISOString(),
        fulfilment_at: new Date(fulfilmentAt.value).toISOString(),
        open_choices: (openChoices.querySelector("input") as HTMLInputElement).checked,
        public_ballots: (publicBallots.querySelector("input") as HTMLInputElement).checked,
      });
      app.navigate(`/votes/${created.id}`);
    });
  }

  update();

  return page(
    t.t("vote.new"),
    section(
      t.t("vote.new"),
      form(
        () => {
          void create();
        },
        el(
          "fieldset",
          { class: "field" },
          el("legend", { class: "field-label" }, `${t.t("vote.restaurants")} `, counter),
          list,
          listProblem.element,
        ),
        field({ label: t.t("vote.decision.label"), control: decisionAt, error: timesError }),
        field({ label: t.t("order.deadline.label"), control: deadlineAt }),
        field({ label: t.t("order.fulfilment.label"), control: fulfilmentAt }),
        hoursWarning,
        el("p", { class: "field-hint", text: t.t("vote.fulfilment_note") }),
        openChoices,
        el("div", {}, publicBallots, el("p", { class: "field-hint", text: t.t("vote.public_ballots.hint") })),
        actions(submit),
        status.element,
      ),
    ),
  );
}
