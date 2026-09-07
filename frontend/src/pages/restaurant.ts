// The restaurant page: the restaurant itself, its contacts, its opening hours
// and its menu, stacked as docs/06_ui_ux.md describes.
//
// Everything here is editable by any logged-in user. Only deletion is the
// administrator's, and only the administrator is offered it.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el, icon } from "../dom";
import { formatWeekday, moneyInputValue, parseMoney } from "../format";
import { button, field, form, input, select, textarea } from "../components/forms";
import { confirmDialog } from "../components/modal";
import { imageField } from "../components/images";
import { contactIcon, contactTarget, type Linkable } from "../contacts";
import { referenceName } from "../i18n";
import { minorUnitOf, referenceData, type ReferenceData } from "../reference";
import { tabs } from "../components/tabs";
import { menuSection } from "./menu";
import { actions, card, page, pageWithActions, section, setPageTitle, statusLine } from "./page";

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

  // Whether anything still points at this restaurant. Deleting one that an
  // order references is refused with 4003, and finding that out by pressing the
  // button and reading an error is a worse answer than a button that says so
  // before it is pressed. The list is small (ADR-0006) and this is one request.
  const blocking = await getList<{ restaurant_id: string }>("/orders", "orders")
    .then((orders) => orders.some((order) => order.restaurant_id === restaurant.id))
    .catch(() => false);

  const data = dataSection(app, reference, restaurant);
  const header = restaurantControls(app, data, blocking);

  const article = pageWithActions(
    restaurant.name,
    header,
    // The menu first: it is what somebody opening a restaurant almost always
    // came for, and it used to be the section they had to scroll past three
    // others to reach.
    tabs(
      [
        { id: "menu", label: t.t("restaurant.menu"), panel: menuSection(app, reference, restaurant) },
        { id: "data", label: t.t("restaurant.data"), panel: data.element },
        {
          id: "contacts",
          label: t.t("restaurant.contacts"),
          panel: contactsSection(app, reference, restaurant),
        },
        {
          id: "hours",
          label: t.t("restaurant.opening_hours"),
          panel: hoursSection(app, restaurant),
        },
      ],
      { label: t.t("restaurant.sections"), initial: 0 },
    ),
  );

  data.onRename((next) => setPageTitle(article, next));
  return article;
}

/**
 * Save and Delete, on the title's line.
 *
 * Save belongs to the restaurant, not to the tab that happens to hold its
 * fields, so it sits with the name and stays reachable from any tab. It is
 * faded until something has actually changed: a Save that is always available
 * says nothing about whether there is anything to save.
 *
 * Delete is faded rather than hidden when it cannot be used, which is a
 * departure from the rule in docs/06_ui_ux.md about not showing controls that
 * would be refused. The difference is that this one explains itself: its title
 * says whether the obstacle is the permission or an order still pointing at the
 * restaurant, and that is more useful than the button silently not being there.
 */
function restaurantControls(app: App, data: DataSection, blocking: boolean): HTMLElement {
  const { t } = app;

  const save = button({ label: t.t("action.save"), variant: "primary" });
  save.disabled = true;
  save.addEventListener("click", () => {
    void data.save();
  });
  data.onDirtyChange((dirty) => {
    save.disabled = !dirty;
  });

  const why = !app.session.isAdmin
    ? t.t("restaurant.delete.admin_only")
    : blocking
      ? t.t("restaurant.delete.in_use")
      : "";

  const remove = button({
    label: t.t("restaurant.delete"),
    variant: "danger",
    ...(why ? { title: why } : {}),
    onclick: () => {
      void data.remove();
    },
  });
  remove.disabled = why !== "";

  return el("div", { class: "page-heading-actions" }, save, remove);
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

/**
 * The restaurant's own fields, and the two operations that act on the whole of
 * it.
 *
 * Save and Delete are handed back rather than rendered here: they live on the
 * page heading now, so that they are reachable whichever tab is open. What the
 * section keeps is the knowledge of *when* saving is worth offering, which is
 * why it reports its dirty state rather than exposing its inputs.
 */
interface DataSection {
  element: HTMLElement;
  save(): Promise<void>;
  remove(): Promise<void>;
  onDirtyChange(listen: (dirty: boolean) => void): void;
  /** Called with the new name after a save that changed it. */
  onRename(listen: (name: string) => void): void;
}

function dataSection(
  app: App,
  reference: ReferenceData,
  restaurant: RestaurantDetail,
): DataSection {
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
      touched();
    },
  });

  // --- has anything changed? ---------------------------------------------
  //
  // Compared against a snapshot rather than tracked with a flag, so typing a
  // character and deleting it again leaves Save disabled: "dirty" should mean
  // the form differs from what was loaded, not that somebody touched a key.
  const snapshot = (): string =>
    JSON.stringify([
      name.value.trim(),
      currency.value,
      minimum.value.trim(),
      fee.value.trim(),
      notes.value.trim(),
      logoImageId,
    ]);

  let saved = snapshot();
  const listeners: ((dirty: boolean) => void)[] = [];
  const renameListeners: ((name: string) => void)[] = [];
  const renamed = (next: string): void => {
    for (const listen of renameListeners) {
      listen(next);
    }
  };
  const touched = (): void => {
    const dirty = snapshot() !== saved;
    for (const listen of listeners) {
      listen(dirty);
    }
  };

  for (const control of [name, currency, minimum, fee, notes]) {
    control.addEventListener("input", touched);
    control.addEventListener("change", touched);
  }

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

  async function save(): Promise<void> {
    status.clear();
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
      // The heading above the form as well as the browser tab. Setting only the
      // tab left the page still displaying the old name until it was reloaded,
      // which reads as a save that did not take.
      renamed(name.value.trim());
      // What was just written becomes the new baseline, so Save goes quiet
      // again until something else changes.
      saved = snapshot();
      touched();
    } catch (error) {
      status.fail(errorMessage(t, error));
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

  const element = card(
    // Still a form, so Enter in a field saves: the button that submits it is on
    // the page heading rather than in here, which a form is perfectly happy
    // with.
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
      status.element,
    ),
  );

  return {
    element,
    save,
    remove,
    onDirtyChange(listen: (dirty: boolean) => void): void {
      listeners.push(listen);
    },
    onRename(listen: (name: string) => void): void {
      renameListeners.push(listen);
    },
  };
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

/** The code of a contact type, by its id. */
function codeOf(reference: ReferenceData, id: string): string {
  return reference.contactTypes.find((type) => type.id === id)?.code ?? "";
}

/** How a contact type wants to be rendered, by its id. */
function renderAsOf(reference: ReferenceData, id: string): string {
  return reference.contactTypes.find((type) => type.id === id)?.render_as ?? "text";
}

/**
 * The button behind a contact's value.
 *
 * It opens the thing the contact is: a telephone number dials, an address opens
 * a map, a website opens in a new tab. The icon says which before it is
 * pressed, so a row of contacts can be scanned rather than read.
 *
 * "Other" is a free string with no sensible target, so it gets no button -- but
 * it keeps the column, so a list of contacts stays a column of fields rather
 * than a ragged edge. That is why this always returns an element and swaps its
 * contents, rather than returning null.
 */
function contactButton(
  app: App,
  read: () => Linkable,
): { element: HTMLElement; refresh: () => void } {
  const element = el("span", { class: "contact-open" });

  const refresh = (): void => {
    const contact = read();
    const target = contactTarget(contact);
    const name = contactIcon(contact);

    if (!target || !name) {
      element.replaceChildren();
      element.classList.add("contact-open-empty");
      return;
    }

    element.classList.remove("contact-open-empty");
    const label = app.t.t("restaurant.contact.open", {
      type: referenceName(app.t, "contact_type", contact.contact_type_code),
    });
    element.replaceChildren(
      el(
        "a",
        {
          class: "button button-icon",
          href: target.href,
          "aria-label": label,
          title: label,
          // A new tab for the two that leave the application, and never
          // without `noreferrer`: a map query carries the restaurant's address
          // and there is no reason to tell Google where the reader came from.
          ...(target.external ? { target: "_blank", rel: "noreferrer" } : {}),
        },
        icon(name),
      ),
    );
  };

  refresh();
  return { element, refresh };
}

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

    // The button behind the value: what this contact opens. It follows the
    // type dropdown live, so switching a number from "phone" to "website"
    // changes what pressing it does without a save in between.
    const open = contactButton(app, () => ({
      contact_type_code: codeOf(reference, type.value),
      render_as: renderAsOf(reference, type.value),
      value: value.value,
    }));
    type.addEventListener("change", open.refresh);
    value.addEventListener("input", open.refresh);

    return el(
      "div",
      { class: "row contact-row" },
      field({ label: t.t("restaurant.contact.type"), control: type }),
      field({ label: t.t("restaurant.contact.value"), control: value }),
      open.element,
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

    // The same grid as an existing row, including the column the open button
    // sits in. It is empty here -- there is nothing to open until the contact
    // exists -- and reserving it is what keeps the new row's fields the same
    // width as the ones above it rather than spreading into the gap.
    return el(
      "div",
      { class: "row row-new contact-row" },
      field({ label: t.t("restaurant.contact.type"), control: type }),
      field({ label: t.t("restaurant.contact.value"), control: value }),
      el("span", { class: "contact-open contact-open-empty", "aria-hidden": "true" }),
      field({
        label: t.t("restaurant.contact.label"),
        control: label,
        optionalLabel: t.t("auth.optional"),
      }),
      actions(add),
    );
  }

  render();
  return card(list, status.element);
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
    //
    // Its column is always there, empty or not. The note appearing and
    // disappearing used to push the row's controls sideways as the times were
    // typed, so a list of opening hours never settled into a shape.
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

  return card(
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
