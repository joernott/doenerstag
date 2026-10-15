// The restaurant's contacts. Part of the restaurant page.

import type { App } from "../../app";
import { api, errorMessage } from "../../api";
import { el, icon } from "../../dom";
import { button, field, input } from "../../components/forms";
import { contactIcon, contactTarget, type Linkable } from "../../contacts";
import { referenceName } from "../../i18n";
import type { ReferenceData } from "../../reference";
import { actions, card } from "../page";
import type { Contact, RestaurantDetail } from "../../types";
import { runAction, statusLine } from "../../components/status";
import { contactTypeSelect } from "./selects";

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
export function contactsSection(
  app: App,
  reference: ReferenceData,
  restaurant: RestaurantDetail,
): HTMLElement {
  const { t } = app;
  const status = statusLine();
  const list = el("div", { class: "rows" });
  let contacts = [...restaurant.contacts];

  function render(): void {
    list.replaceChildren(...contacts.map((contact) => contactRow(contact)), newContactRow());
  }

  function contactRow(contact: Contact): HTMLElement {
    const type = contactTypeSelect(app, reference, contact.contact_type_id);
    const value = input({ value: contact.value, required: true });
    const label = input({ value: contact.label });

    const save = button({
      label: t.t("action.save"),
      variant: "primary",
      onclick: () => store(),
    });
    const remove = button({
      label: t.t("action.remove"),
      variant: "danger",
      disabled: contacts.length < 2,
      ...(contacts.length < 2 ? { title: t.t("restaurant.contact.last") } : {}),
      onclick: () => drop(),
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
      await runAction({ status, t }, async () => {
        await api.delete(`/restaurants/${restaurant.id}/contacts/${contact.id}`);
        contacts = contacts.filter((entry) => entry.id !== contact.id);
        render();
      });
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
      variant: "primary",
      onclick: () => create(),
    });

    async function create(): Promise<void> {
      status.clear();
      if (value.value.trim() === "") {
        value.focus();
        return;
      }
      await runAction({ status, t }, async () => {
        const created = await api.post<Contact>(`/restaurants/${restaurant.id}/contacts`, {
          contact_type_id: type.value,
          value: value.value.trim(),
          label: label.value.trim(),
          sort_order: (contacts[contacts.length - 1]?.sort_order ?? 0) + 10,
        });
        contacts.push(created);
        render();
      });
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
      el("span", {
        class: "contact-open contact-open-empty",
        "aria-hidden": "true",
      }),
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
