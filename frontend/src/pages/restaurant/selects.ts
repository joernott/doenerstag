// The selectors the create form shares with the sections that edit.

import type { App } from "../../app";
import { select } from "../../components/forms";
import { referenceName } from "../../i18n";
import type { ReferenceData } from "../../reference";

export function currencySelect(
  app: App,
  reference: ReferenceData,
  current: string,
): HTMLSelectElement {
  const choices = reference.currencies.map((currency) => ({
    value: currency.code,
    label: `${referenceName(app.t, "currency", currency.code)} (${currency.symbol})`,
  }));
  return select(choices, { name: "currency_code", value: current });
}

/** A contact type selector, showing each type's translated name. */
export function contactTypeSelect(
  app: App,
  reference: ReferenceData,
  current?: string,
): HTMLSelectElement {
  const choices = reference.contactTypes.map((type) => ({
    value: type.id,
    label: referenceName(app.t, "contact_type", type.code),
  }));
  return select(choices, {
    name: "contact_type_id",
    ...(current ? { value: current } : {}),
  });
}
