// The reference tables, fetched once.
//
// Currencies, contact types, tags, allergens and additives change only when a
// migration changes them, and a restaurant page needs four of the five to
// render one form. Fetching them per form would be four requests per keystroke
// of a page that is meant to feel instant, so they are fetched once per page
// load and kept.
//
// None of them carries a display name: the API sends codes and the catalog
// translates them (docs/07_i18n.md). Only `tag` has a name column, and only for
// tags a user invented.

import { getList } from "./api";

export interface Currency {
  code: string;
  symbol: string;
  minor_unit: number;
  sort_order: number;
}

export interface ContactType {
  id: string;
  code: string;
  render_as: string;
  sort_order: number;
}

export interface Tag {
  id: string;
  code: string;
  name: string;
  sort_order: number;
}

export interface Classification {
  id: string;
  code: string;
  reference: string;
  sort_order: number;
}

export interface ReferenceData {
  currencies: Currency[];
  contactTypes: ContactType[];
  tags: Tag[];
  allergens: Classification[];
  additives: Classification[];
}

let pending: Promise<ReferenceData> | null = null;

/**
 * Loads the reference tables, once.
 *
 * The promise is cached rather than the result, so two components asking at the
 * same time share one set of requests instead of racing to start a second.
 */
export function referenceData(): Promise<ReferenceData> {
  pending ??= Promise.all([
    getList<Currency>("/currencies", "currencies"),
    getList<ContactType>("/contact-types", "contact_types"),
    getList<Tag>("/tags", "tags"),
    getList<Classification>("/allergens", "allergens"),
    getList<Classification>("/additives", "additives"),
  ])
    .then(([currencies, contactTypes, tags, allergens, additives]) => ({
      currencies,
      contactTypes,
      tags,
      allergens,
      additives,
    }))
    .catch((error: unknown) => {
      // A failed load must not be remembered as the answer, or the page would
      // stay broken until it is reloaded.
      pending = null;
      throw error;
    });

  return pending;
}

/** Forgets what was loaded. For the tests, and after a tag is created. */
export function resetReferenceData(): void {
  pending = null;
}

/**
 * How many decimal places a currency has.
 *
 * Two, unless the reference table says otherwise -- and it does, for the
 * currencies that have none. Falling back to two for an unknown code is right
 * for every currency this application is likely to meet.
 */
export function minorUnitOf(currencies: readonly Currency[], code: string): number {
  return currencies.find((currency) => currency.code === code)?.minor_unit ?? 2;
}
