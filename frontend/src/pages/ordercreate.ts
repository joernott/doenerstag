// Creating an order.
//
// F5.1: a restaurant, a fulfilment type, a time the food arrives and a deadline
// for joining. Three checks happen here before anything is sent: the deadline
// must be strictly before the fulfilment time (F5.3) and must not already have
// passed (F5.3a), both of which are errors, and the food should arrive while the
// restaurant is open (F5.4), which is only a warning -- restaurants do accept
// pre-orders.
//
// The form opens on an hour and two hours from now.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { accountChoices, listAccounts } from "../accounts";
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

/**
 * A moment a given number of hours from now, which is what the form starts
 * from.
 *
 * The form used to default to today at 11:00 and 12:00 -- a lunch order, on the
 * assumption that it is being opened in the morning. Opened at ten in the
 * evening it offered a deadline eleven hours in the past, and the order created
 * from it was closed before it existed. An hour and two hours from now is right
 * whatever the time is.
 */
function inHours(hours: number): Date {
  return new Date(Date.now() + hours * 60 * 60 * 1000);
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

  // Who might collect the money or fetch the food. A failure here leaves the
  // form usable with nobody in either job, which is what a new order starts
  // with anyway.
  const accounts = await listAccounts().catch(() => []);
  const people = accountChoices(accounts, t.t("order.nobody"));

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
    value: localInputValue(inHours(2)),
    required: true,
  });
  const deadlineAt = input({
    type: "datetime-local",
    name: "deadline_at",
    value: localInputValue(inHours(1)),
    required: true,
  });
  const moneyCollector = select(people, { name: "money_collector_id" });
  const pickupPerson = select(people, { name: "pickup_person_id" });

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

    // Two hard rules. F5.3: the deadline is strictly before the fulfilment
    // time. And it has not already passed -- an order created closed is one
    // nobody can add anything to, because F6.6 makes an expired order read-only
    // for its creator too. The server refuses both; these say so before the
    // request rather than after it.
    const ordered = known && closes.getTime() < fulfils.getTime();
    const future = known && closes.getTime() > Date.now();

    deadlineProblem.textContent = !ordered
      ? t.t("order.deadline_before_fulfilment")
      : future
        ? ""
        : t.t("error.1014");

    // F5.4: a warning, never a refusal.
    hoursWarning.textContent =
      known && hours.length > 0 && !isOpen(hours, fulfils) ? t.t("order.outside_hours") : "";

    return ordered && future;
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
        money_collector_id: moneyCollector.value,
        pickup_person_id: pickupPerson.value,
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
