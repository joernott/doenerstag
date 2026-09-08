// Creating an order.
//
// F5.1: a restaurant, a fulfilment type, a time the food arrives and a deadline
// for joining. Two checks happen here before anything is sent: the deadline
// must be strictly before the fulfilment time (F5.3), which is an error, and
// the food should arrive while the restaurant is open (F5.4), which is only a
// warning -- restaurants do accept pre-orders.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el } from "../dom";
import { button, field, form, input, select } from "../components/forms";
import { isOpen } from "../openinghours";
import { actions, page, section, statusLine } from "./page";
import type { Contact, OpeningPeriod, Restaurant } from "./restaurant";

interface RestaurantDetail extends Restaurant {
  contacts: Contact[];
  opening_hours: OpeningPeriod[];
}

/** A `datetime-local` value for a Date, in the viewer's own time zone. */
export function localInputValue(date: Date): string {
  const pad = (value: number): string => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}`
  );
}

/** Today at a given hour, which is what the form starts from. */
function todayAt(hours: number, minutes = 0): Date {
  const date = new Date();
  date.setHours(hours, minutes, 0, 0);
  return date;
}

export async function createOrderPage(app: App): Promise<HTMLElement> {
  const { t } = app;

  let restaurants: Restaurant[];
  try {
    restaurants = await getList<Restaurant>("/restaurants", "restaurants");
  } catch (error) {
    return page(t.t("order.new"), el("p", { class: "field-error", text: errorMessage(t, error) }));
  }

  if (restaurants.length === 0) {
    // Nothing to order from. Sending somebody to a form whose first field has
    // no options would be worse than saying so.
    return page(
      t.t("order.new"),
      el("p", { class: "muted", text: t.t("order.no_restaurants") }),
      el("p", {}, el("a", { class: "link", href: "/restaurants", text: t.t("restaurant.new") })),
    );
  }

  restaurants.sort((left, right) => left.name.localeCompare(right.name, app.language));

  const status = statusLine();

  const restaurant = select(
    restaurants.map((entry) => ({ value: entry.id, label: entry.name })),
    { name: "restaurant_id" },
  );
  const fulfilment = select(
    [
      { value: "pickup", label: t.t("order.fulfilment.pickup") },
      { value: "delivery", label: t.t("order.fulfilment.delivery") },
    ],
    { name: "fulfilment" },
  );
  const fulfilmentAt = input({
    type: "datetime-local",
    name: "fulfilment_at",
    value: localInputValue(todayAt(12)),
    required: true,
  });
  const deadlineAt = input({
    type: "datetime-local",
    name: "deadline_at",
    value: localInputValue(todayAt(11)),
    required: true,
  });
  const moneyCollector = input({ name: "money_collector" });
  const pickupPerson = input({ name: "pickup_person" });

  const deadlineProblem = el("p", { class: "field-error", role: "alert" });
  const hoursWarning = el("p", { class: "field-hint" });

  // The opening hours of whichever restaurant is selected, for the warning.
  let hours: OpeningPeriod[] = [];
  const loadHours = async (): Promise<void> => {
    try {
      const detail = await api.get<RestaurantDetail>(`/restaurants/${restaurant.value}`);
      hours = detail.opening_hours;
    } catch {
      hours = [];
    }
    validate();
  };

  function validate(): boolean {
    const fulfils = new Date(fulfilmentAt.value);
    const closes = new Date(deadlineAt.value);
    const known = !Number.isNaN(fulfils.getTime()) && !Number.isNaN(closes.getTime());

    // F5.3, and the only hard rule on this form.
    const ordered = known && closes.getTime() < fulfils.getTime();
    deadlineProblem.textContent = ordered ? "" : t.t("order.deadline_before_fulfilment");

    // F5.4: a warning, never a refusal.
    hoursWarning.textContent =
      known && hours.length > 0 && !isOpen(hours, fulfils) ? t.t("order.outside_hours") : "";

    return ordered;
  }

  for (const control of [fulfilmentAt, deadlineAt]) {
    control.addEventListener("input", () => {
      validate();
    });
  }
  restaurant.addEventListener("change", () => {
    void loadHours();
  });
  void loadHours();

  const submit = button({ label: t.t("order.new"), variant: "primary", type: "submit" });

  async function create(): Promise<void> {
    status.clear();
    if (!validate()) {
      deadlineAt.focus();
      return;
    }

    submit.disabled = true;
    try {
      const created = await api.post<{ id: string }>("/orders", {
        restaurant_id: restaurant.value,
        fulfilment: fulfilment.value,
        // The API takes UTC; the field is the viewer's own wall clock.
        fulfilment_at: new Date(fulfilmentAt.value).toISOString(),
        deadline_at: new Date(deadlineAt.value).toISOString(),
        money_collector: moneyCollector.value.trim(),
        pickup_person: pickupPerson.value.trim(),
      });
      app.navigate(`/orders/${created.id}`);
    } catch (error) {
      status.fail(errorMessage(t, error));
    } finally {
      submit.disabled = false;
    }
  }

  return page(
    t.t("order.new"),
    section(
      t.t("order.new"),
      form(
        () => {
          void create();
        },
        field({ label: t.t("restaurant.data"), control: restaurant }),
        field({ label: t.t("order.fulfilment.type"), control: fulfilment }),
        field({ label: t.t("order.fulfilment.label"), control: fulfilmentAt }),
        hoursWarning,
        field({ label: t.t("order.deadline.label"), control: deadlineAt }),
        deadlineProblem,
        field({
          label: t.t("order.money_collector"),
          control: moneyCollector,
          optionalLabel: t.t("auth.optional"),
        }),
        field({
          label: t.t("order.pickup_person"),
          control: pickupPerson,
          optionalLabel: t.t("auth.optional"),
        }),
        actions(submit),
        status.element,
      ),
    ),
  );
}
