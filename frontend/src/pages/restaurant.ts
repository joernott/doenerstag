// The restaurant page: the restaurant itself, its contacts, its opening hours
// and its menu, stacked as docs/06_ui_ux.md describes.
//
// Everything here is editable by any logged-in user. Only deletion is the
// administrator's, and only the administrator is offered it.

import type { App } from "../app";
import { api, errorMessage } from "../api";
import { el, type Child } from "../dom";
import { formatWeekday, moneyInputValue, parseMoney } from "../format";
import { button, field, form, input, select, textarea } from "../components/forms";
import { confirmDialog } from "../components/modal";
import { imageField } from "../components/images";
import { referenceName } from "../i18n";
import { minorUnitOf, referenceData, type ReferenceData } from "../reference";
import { menuSection } from "./menu";
import { actions, page, section, statusLine } from "./page";

/** A restaurant as the API sends it. */
export interface Restaurant {
  id: string;
  name: string;
  logo_image_id: string | null;
  currency_code: string;
  min_order_value_cents: number | null;
  delivery_fee_cents: number | null;
  notes: string;
}

export interface Contact {
  id: string;
  contact_type_id: string;
  contact_type_code: string;
  render_as: string;
  value: string;
  label: string;
  sort_order: number;
}

export interface OpeningPeriod {
  id: string;
  day_of_week: number;
  start: string;
  end: string;
  crosses_midnight: boolean;
}

interface RestaurantDetail extends Restaurant {
  contacts: Contact[];
  opening_hours: OpeningPeriod[];
}

/** The page for one restaurant, or the form that creates one. */
export async function restaurantPage(app: App, id: string): Promise<HTMLElement> {
  const { t } = app;

  let reference: ReferenceData;
  try {
    reference = await referenceData();
  } catch (error) {
    return page(t.t("nav.restaurants"), el("p", { class: "field-error", text: errorMessage(t, error) }));
  }

  if (id === "new") {
    return createPage(app, reference);
  }

  let restaurant: RestaurantDetail;
  try {
    restaurant = await api.get<RestaurantDetail>(`/restaurants/${id}`);
  } catch (error) {
    return page(t.t("nav.restaurants"), el("p", { class: "field-error", text: errorMessage(t, error) }));
  }

  return page(
    restaurant.name,
    dataSection(app, reference, restaurant),
    contactsSection(app, reference, restaurant),
    hoursSection(app, restaurant),
    menuSection(app, reference, restaurant),
  );
}

// --- creating ----------------------------------------------------------------

/**
 * Creating a restaurant.
 *
 * A separate, smaller form: a restaurant needs a name, a currency and at least
 * one contact before it can exist at all (error 1007), and asking for opening
 * hours and a menu in the same breath would be asking for everything at once.
 * Everything else is edited on the page this redirects to.
 */
function createPage(app: App, reference: ReferenceData): HTMLElement {
  const { t } = app;
  const status = statusLine();

  const name = input({ name: "name", required: true });
  const currency = currencySelect(app, reference, "EUR");
  const contactType = contactTypeSelect(app, reference);
  const contactValue = input({ name: "contact_value", required: true });
  const submit = button({ label: t.t("action.create"), variant: "primary", type: "submit" });

  async function create(): Promise<void> {
    status.clear();
    submit.disabled = true;
    try {
      const created = await api.post<Restaurant>("/restaurants", {
        name: name.value.trim(),
        currency_code: currency.value,
        contacts: [
          {
            contact_type_id: contactType.value,
            value: contactValue.value.trim(),
            label: "",
            sort_order: 10,
          },
        ],
      });
      app.navigate(`/restaurants/${created.id}`);
    } catch (error) {
      status.fail(errorMessage(t, error));
    } finally {
      submit.disabled = false;
    }
  }

  return page(
    t.t("restaurant.new"),
    section(
      t.t("restaurant.data"),
      form(
        () => {
          void create();
        },
        field({ label: t.t("restaurant.name"), control: name }),
        field({ label: t.t("restaurant.currency"), control: currency }),
        field({ label: t.t("restaurant.contact.type"), control: contactType }),
        field({
          label: t.t("restaurant.contact.value"),
          control: contactValue,
          hint: t.t("restaurant.contact.last"),
        }),
        actions(submit),
        status.element,
      ),
    ),
  );
}

// --- the restaurant itself ---------------------------------------------------

function dataSection(
  app: App,
  reference: ReferenceData,
  restaurant: RestaurantDetail,
): HTMLElement {
  const { t } = app;
  const status = statusLine();
  let minorUnit = minorUnitOf(reference.currencies, restaurant.currency_code);

  const name = input({ name: "name", value: restaurant.name, required: true });
  const currency = currencySelect(app, reference, restaurant.currency_code);
  const minimum = input({
    name: "min_order_value",
    inputMode: "decimal",
    value:
      restaurant.min_order_value_cents === null
        ? ""
        : moneyInputValue(restaurant.min_order_value_cents, minorUnit),
  });
  const fee = input({
    name: "delivery_fee",
    inputMode: "decimal",
    value:
      restaurant.delivery_fee_cents === null
        ? ""
        : moneyInputValue(restaurant.delivery_fee_cents, minorUnit),
  });
  const notes = textarea({ name: "notes", value: restaurant.notes, rows: 3 });

  let logoImageId = restaurant.logo_image_id;
  const logo = imageField({
    t,
    locale: app.language,
    label: t.t("restaurant.logo"),
    imageId: logoImageId,
    limits: app.version,
    onChange: (id) => {
      logoImageId = id;
    },
  });

  // Changing the currency changes what the two money fields mean, so they are
  // re-rendered in the new currency's minor unit rather than silently keeping
  // digits that meant something else.
  currency.addEventListener("change", () => {
    const next = minorUnitOf(reference.currencies, currency.value);
    for (const money of [minimum, fee]) {
      const parsed = parseMoney(money.value, minorUnit);
      money.value = parsed === null ? "" : moneyInputValue(parsed, next);
    }
    minorUnit = next;
  });

  const submit = button({ label: t.t("action.save"), variant: "primary", type: "submit" });

  async function save(): Promise<void> {
    status.clear();
    submit.disabled = true;
    try {
      await api.patch(`/restaurants/${restaurant.id}`, {
        name: name.value.trim(),
        currency_code: currency.value,
        logo_image_id: logoImageId,
        min_order_value_cents: parseMoney(minimum.value, minorUnit),
        delivery_fee_cents: parseMoney(fee.value, minorUnit),
        notes: notes.value.trim(),
      });
      status.say(t.t("state.saved"));
      document.title = `${name.value.trim()} — doenerstag`;
    } catch (error) {
      status.fail(errorMessage(t, error));
    } finally {
      submit.disabled = false;
    }
  }

  async function remove(): Promise<void> {
    const agreed = await confirmDialog({
      t,
      message: t.t("restaurant.delete.confirm"),
      detail: restaurant.name,
      confirmLabel: t.t("action.delete"),
    });
    if (!agreed) {
      return;
    }
    try {
      await api.delete(`/restaurants/${restaurant.id}`);
      app.navigate("/restaurants");
    } catch (error) {
      // 4003 when an order still references it, which is a real answer and not
      // a failure of the page: the message says so.
      status.fail(errorMessage(t, error));
    }
  }

  const controls: Child[] = [submit];
  if (app.session.isAdmin) {
    controls.push(
      button({
        label: t.t("restaurant.delete"),
        variant: "danger",
        onclick: () => {
          void remove();
        },
      }),
    );
  }

  return section(
    t.t("restaurant.data"),
    form(
      () => {
        void save();
      },
      field({ label: t.t("restaurant.name"), control: name }),
      field({ label: t.t("restaurant.currency"), control: currency }),
      logo.element,
      field({
        label: t.t("restaurant.min_order_value"),
        control: minimum,
        optionalLabel: t.t("auth.optional"),
      }),
      field({
        label: t.t("restaurant.delivery_fee"),
        control: fee,
        optionalLabel: t.t("auth.optional"),
      }),
      field({
        label: t.t("restaurant.notes"),
        control: notes,
        optionalLabel: t.t("auth.optional"),
      }),
      actions(...controls),
      status.element,
    ),
  );
}

/** A currency selector, showing each currency's translated name and symbol. */
function currencySelect(app: App, reference: ReferenceData, current: string): HTMLSelectElement {
  const choices = reference.currencies.map((currency) => ({
    value: currency.code,
    label: `${referenceName(app.t, "currency", currency.code)} (${currency.symbol})`,
  }));
  return select(choices, { name: "currency_code", value: current });
}

function contactTypeSelect(
  app: App,
  reference: ReferenceData,
  current?: string,
): HTMLSelectElement {
  const choices = reference.contactTypes.map((type) => ({
    value: type.id,
    label: referenceName(app.t, "contact_type", type.code),
  }));
  return select(choices, { name: "contact_type_id", ...(current ? { value: current } : {}) });
}

// --- contacts ----------------------------------------------------------------

/**
 * The contacts editor.
 *
 * Each row saves itself, because a contact is a resource of its own and the
 * API has no "replace them all" call for contacts the way it does for opening
 * hours. The delete button on the last remaining row is disabled with the
 * reason, rather than being offered and then refused with error 1007.
 */
function contactsSection(
  app: App,
  reference: ReferenceData,
  restaurant: RestaurantDetail,
): HTMLElement {
  const { t } = app;
  const status = statusLine();
  const list = el("div", { class: "rows" });
  let contacts = [...restaurant.contacts];

  function render(): void {
    list.replaceChildren(
      ...contacts.map((contact) => contactRow(contact)),
      newContactRow(),
    );
  }

  function contactRow(contact: Contact): HTMLElement {
    const type = contactTypeSelect(app, reference, contact.contact_type_id);
    const value = input({ value: contact.value, required: true });
    const label = input({ value: contact.label });

    const save = button({
      label: t.t("action.save"),
      onclick: () => {
        void store();
      },
    });
    const remove = button({
      label: t.t("action.remove"),
      variant: "danger",
      disabled: contacts.length < 2,
      ...(contacts.length < 2 ? { title: t.t("restaurant.contact.last") } : {}),
      onclick: () => {
        void drop();
      },
    });

    async function store(): Promise<void> {
      status.clear();
      try {
        await api.put(`/restaurants/${restaurant.id}/contacts/${contact.id}`, {
          contact_type_id: type.value,
          value: value.value.trim(),
          label: label.value.trim(),
          sort_order: contact.sort_order,
        });
        status.say(t.t("state.saved"));
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    }

    async function drop(): Promise<void> {
      status.clear();
      try {
        await api.delete(`/restaurants/${restaurant.id}/contacts/${contact.id}`);
        contacts = contacts.filter((entry) => entry.id !== contact.id);
        render();
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    }

    return el(
      "div",
      { class: "row" },
      field({ label: t.t("restaurant.contact.type"), control: type }),
      field({ label: t.t("restaurant.contact.value"), control: value }),
      field({
        label: t.t("restaurant.contact.label"),
        control: label,
        optionalLabel: t.t("auth.optional"),
      }),
      actions(save, remove),
    );
  }

  function newContactRow(): HTMLElement {
    const type = contactTypeSelect(app, reference);
    const value = input({});
    const label = input({});

    const add = button({
      label: t.t("restaurant.contact.add"),
      onclick: () => {
        void create();
      },
    });

    async function create(): Promise<void> {
      status.clear();
      if (value.value.trim() === "") {
        value.focus();
        return;
      }
      try {
        const created = await api.post<Contact>(`/restaurants/${restaurant.id}/contacts`, {
          contact_type_id: type.value,
          value: value.value.trim(),
          label: label.value.trim(),
          sort_order: (contacts[contacts.length - 1]?.sort_order ?? 0) + 10,
        });
        contacts.push(created);
        render();
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    }

    return el(
      "div",
      { class: "row row-new" },
      field({ label: t.t("restaurant.contact.type"), control: type }),
      field({ label: t.t("restaurant.contact.value"), control: value }),
      field({
        label: t.t("restaurant.contact.label"),
        control: label,
        optionalLabel: t.t("auth.optional"),
      }),
      actions(add),
    );
  }

  render();
  return section(t.t("restaurant.contacts"), list, status.element);
}

// --- opening hours -----------------------------------------------------------

/**
 * The opening hours editor.
 *
 * The whole set is replaced in one PUT, which is what the API offers and what
 * matches how a person edits them: a lunch break is two rows that only make
 * sense together.
 */
function hoursSection(app: App, restaurant: RestaurantDetail): HTMLElement {
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
    const hint = el("span", { class: "hint-inline" });
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
        { class: "row" },
        field({ label: t.t("restaurant.hours.day"), control: day }),
        field({ label: t.t("restaurant.hours.from"), control: start }),
        field({ label: t.t("restaurant.hours.to"), control: end }),
        hint,
        actions(
          button({
            label: t.t("action.remove"),
            variant: "quiet",
            onclick: () => {
              const index = rows.indexOf(row);
              if (index >= 0) {
                rows.splice(index, 1);
                row.element.remove();
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

  const submit = button({ label: t.t("action.save"), variant: "primary" });
  submit.addEventListener("click", () => {
    void save();
  });

  async function save(): Promise<void> {
    status.clear();
    submit.disabled = true;
    try {
      await api.put(`/restaurants/${restaurant.id}/opening-hours`, {
        opening_hours: rows.map((row) => ({
          day_of_week: Number.parseInt(row.day.value, 10),
          start: row.start.value,
          end: row.end.value,
        })),
      });
      status.say(t.t("state.saved"));
    } catch (error) {
      status.fail(errorMessage(t, error));
    } finally {
      submit.disabled = false;
    }
  }

  return section(
    t.t("restaurant.opening_hours"),
    list,
    actions(
      button({
        label: t.t("restaurant.hours.add"),
        onclick: () => {
          // The "none" message is not a row and must go when one appears.
          list.querySelector(".muted")?.remove();
          addRow();
        },
      }),
      submit,
    ),
    status.element,
  );
}
