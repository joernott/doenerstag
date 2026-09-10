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
import { DEFAULT_MONEY_FORMAT, type MoneyFormat } from "./format";

export interface Currency {
  code: string;
  symbol: string;
  minor_unit: number;
  /** Absent from an installation whose reference data predates migration 10. */
  minor_per_major?: number;
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
 * How to write and divide the given currency.
 *
 * Falling back to two decimal places and a hundred to one for an unknown code
 * is right for every currency this application is likely to meet, and is what
 * it did before the reference table was consulted at all.
 */
export function moneyFormatOf(currencies: readonly Currency[], code: string): MoneyFormat {
  const currency = currencies.find((candidate) => candidate.code === code);
  if (!currency) {
    return DEFAULT_MONEY_FORMAT;
  }
  return {
    digits: currency.minor_unit,
    // An installation whose reference data predates the column sends no ratio
    // at all. Ten to the power of the digits is what this used to assume, and
    // it is right for every currency that installation can name.
    perMajor: currency.minor_per_major ?? 10 ** currency.minor_unit,
  };
}
