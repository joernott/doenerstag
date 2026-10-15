// Creating a restaurant. Part of the restaurant page (20.29).

import type { App } from "../../app";
import { api } from "../../api";
import { button, field, form, input } from "../../components/forms";
import type { ReferenceData } from "../../reference";
import { actions, page, section } from "../page";
import type { Restaurant } from "../../types";
import { runAction, statusLine } from "../../components/status";
import { contactTypeSelect, currencySelect } from "./selects";

/**
 * Creating a restaurant.
 *
 * A separate, smaller form: a restaurant needs a name, a currency and at least
 * one contact before it can exist at all (error 1007), and asking for opening
 * hours and a menu in the same breath would be asking for everything at once.
 * Everything else is edited on the page this redirects to.
 */
export function createPage(app: App, reference: ReferenceData): HTMLElement {
  const { t } = app;
  const status = statusLine();

  const name = input({ name: "name", required: true });
  const currency = currencySelect(app, reference, "EUR");
  const contactType = contactTypeSelect(app, reference);
  const contactValue = input({ name: "contact_value", required: true });
  const submit = button({
    label: t.t("action.create"),
    variant: "primary",
    type: "submit",
  });

  async function create(): Promise<void> {
    await runAction({ status, t, busy: submit }, async () => {
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
    });
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
