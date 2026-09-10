// Locale-aware formatting, all of it through the browser's Intl API.
//
// Everything takes the locale as an argument rather than reading a global, so a
// test can check the German rendering without switching anything, and a page
// can render one restaurant's prices while the interface is in another
// language. See docs/07_i18n.md.

/**
 * How a currency is written and how it divides.
 *
 * Two numbers rather than one, and they are not the same question:
 *
 *   - `digits` is how many decimal places an amount is written with.
 *   - `perMajor` is how many minor units make one major unit.
 *
 * For every currency but two, the second is ten to the power of the first, and
 * this application derived it that way until it met the Malagasy ariary. An
 * ariary is five iraimbilanja and an ouguiya is five khoums -- the only
 * non-decimal currencies still in use -- so ten iraimbilanja are two ariary and
 * not one. Deriving the divisor from the number of written places gets that
 * wrong by a factor of two, which is exactly the kind of wrong nobody notices.
 *
 * The two travel together because passing them separately means eventually
 * passing them in the wrong order.
 */
export interface MoneyFormat {
  digits: number;
  perMajor: number;
}

/** Euros, and anything else two-decimal: the fallback for an unknown code. */
export const DEFAULT_MONEY_FORMAT: MoneyFormat = { digits: 2, perMajor: 100 };

/**
 * Formatters are expensive to construct and are built once per distinct set of
 * options. A page rendering thirty menu items would otherwise build thirty
 * identical formatters.
 */
const cache = new Map<string, Intl.DateTimeFormat | Intl.NumberFormat | Intl.RelativeTimeFormat>();

function cached<T>(key: string, build: () => T): T {
  const existing = cache.get(key);
  if (existing) {
    return existing as T;
  }
  const created = build();
  cache.set(key, created as never);
  return created;
}

/**
 * What to show for a value that is not a timestamp at all.
 *
 * Intl throws on an invalid date, and a throw inside a render takes the whole
 * page with it. That is not hypothetical: a development build reports its build
 * date as "unknown", and the version page rendered nothing at all until this
 * existed. Showing the value as it stands says more than an empty screen.
 */
function unformattable(value: Date | string, date: Date): string | null {
  if (!Number.isNaN(date.getTime())) {
    return null;
  }
  return typeof value === "string" ? value : "";
}

/** Formats a date: `Sep 10, 2026` in English, `10.09.2026` in German. */
export function formatDate(locale: string, value: Date | string): string {
  const date = toDate(value);
  return (
    unformattable(value, date) ??
    cached(`d:${locale}`, () => new Intl.DateTimeFormat(locale, { dateStyle: "medium" })).format(
      date,
    )
  );
}

/** Formats a time. The locale decides 12- or 24-hour, not this code. */
export function formatTime(locale: string, value: Date | string): string {
  const date = toDate(value);
  return (
    unformattable(value, date) ??
    cached(`t:${locale}`, () => new Intl.DateTimeFormat(locale, { timeStyle: "short" })).format(date)
  );
}

/** Formats a date and time together, for a deadline or a fulfilment time. */
export function formatDateTime(locale: string, value: Date | string): string {
  const date = toDate(value);
  return (
    unformattable(value, date) ??
    cached(`dt:${locale}`, () =>
      new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }),
    ).format(date)
  );
}

/** A weekday name, for opening hours. ISO numbering: 1 is Monday. */
export function formatWeekday(locale: string, isoDay: number): string {
  // 2024-01-01 was a Monday, so adding isoDay - 1 lands on the right weekday
  // whatever the number. Using a fixed known date avoids any dependence on
  // when the code runs.
  const reference = new Date(Date.UTC(2024, 0, isoDay, 12));
  return cached(`w:${locale}`, () =>
    new Intl.DateTimeFormat(locale, { weekday: "long", timeZone: "UTC" }),
  ).format(reference);
}

/**
 * Formats an opening-hours time, which is a wall-clock `HH:MM` with no date.
 *
 * Displayed as stored, without time zone conversion: a restaurant that opens at
 * 11:00 opens at 11:00 wherever the viewer is. Only the separator follows the
 * locale, which is why this goes through Intl at all rather than returning the
 * string unchanged.
 */
export function formatClockTime(locale: string, value: string): string {
  const [hours, minutes] = value.split(":").map((part) => Number.parseInt(part, 10));
  if (Number.isNaN(hours) || Number.isNaN(minutes)) {
    return value;
  }
  const reference = new Date(Date.UTC(2024, 0, 1, hours, minutes));
  return cached(`ct:${locale}`, () =>
    new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", timeZone: "UTC" }),
  ).format(reference);
}

/** Formats a plain number. */
export function formatNumber(locale: string, value: number): string {
  return cached(`n:${locale}`, () => new Intl.NumberFormat(locale)).format(value);
}

/**
 * Formats money from the integer minor units the API carries.
 *
 * Two things decide the result, and they are deliberately different:
 *
 *   - The **currency** comes from the restaurant and never follows the
 *     interface language. A restaurant priced in CHF shows CHF to everyone.
 *   - The **number formatting** follows the interface locale, so the same CHF
 *     amount reads `CHF 6.50` in English and `6.50 CHF` in German.
 *
 * The division happens here, immediately before formatting, and nowhere
 * earlier. Every sum in the application is computed on the integers, because a
 * float that has been through three additions is not the number anybody agreed
 * to pay.
 */
export function formatMoney(
  locale: string,
  minorUnits: number,
  currency: string,
  money: MoneyFormat = DEFAULT_MONEY_FORMAT,
): string {
  const { digits, perMajor } = money;

  const formatter = cached(`m:${locale}:${currency}:${digits}`, () => {
    try {
      return new Intl.NumberFormat(locale, {
        style: "currency",
        currency,
        minimumFractionDigits: digits,
        maximumFractionDigits: digits,
      });
    } catch {
      // An unknown currency code makes Intl throw. Falling back to a plain
      // number with the code appended is ugly but readable, and better than a
      // page that fails to render because a restaurant has an odd currency.
      return new Intl.NumberFormat(locale, {
        minimumFractionDigits: digits,
        maximumFractionDigits: digits,
      });
    }
  });

  // Divided by what the currency actually divides into, which is the whole
  // point: ten iraimbilanja are two ariary, and 10 / 10 ** 1 would say one.
  const amount = minorUnits / perMajor;
  const formatted = formatter.format(amount);

  // The fallback path produced no currency, so add the code where a symbol
  // would have been.
  return formatter.resolvedOptions().style === "currency"
    ? formatted
    : `${formatted} ${currency}`;
}

/**
 * Reads an amount typed by a person into the integer minor units the API wants.
 *
 * Done on the string rather than through parseFloat, because the obvious
 * `Math.round(parseFloat(text) * 100)` is wrong: 19.99 is not representable in
 * binary floating point, and the multiplication lands at 1998.9999999999998.
 * Rounding hides it for most numbers and not for all of them, and a price that
 * is one cent out in one restaurant out of fifty is the worst kind of bug.
 *
 * Both separators are accepted whatever the interface language, because a
 * German keyboard's numeric pad produces a comma and a person typing a price
 * should not have to think about which one this field wants. Returns null for
 * anything that is not a number.
 */
export function parseMoney(
  text: string,
  money: MoneyFormat = DEFAULT_MONEY_FORMAT,
): number | null {
  const { digits, perMajor } = money;
  const trimmed = text.trim().replace(/\s/gu, "");
  if (trimmed === "") {
    return null;
  }

  const match = /^(-?)(\d*)(?:[.,](\d*))?$/u.exec(trimmed.replace(/[^\d.,-]/gu, ""));
  if (!match) {
    return null;
  }

  const [, sign, whole = "", fraction = ""] = match;
  if (whole === "" && fraction === "") {
    return null;
  }

  // Pad a short fraction and cut a long one. Cutting rather than rounding: a
  // third decimal in a price is a typing slip, and quietly rounding it up would
  // charge somebody a cent they never agreed to.
  const scaled = fraction.padEnd(digits, "0").slice(0, digits);

  // What was typed, as an integer number of the smallest written place: "2.4"
  // with one digit is 24 tenths. Then tenths to minor units, which for a
  // decimal currency is a no-op -- 1999 hundredths at 100 per major over 10**2
  // is 1999 cents -- and for the ariary is the conversion that matters: 24
  // tenths at 5 per major over 10**1 is 12 iraimbilanja.
  //
  // Rounded rather than truncated, because a currency whose divisor is not the
  // written scale has amounts that fall between its own places: 2.3 ariary
  // cannot be paid, and the nearest thing that can is 2.4.
  const written = Number.parseInt((whole || "0") + scaled, 10);
  const value = Math.round((written * perMajor) / 10 ** digits);
  return sign === "-" ? -value : value;
}

/**
 * Renders minor units for an input field: a plain number, no currency, no
 * grouping separators, because the value goes back through parseMoney.
 */
export function moneyInputValue(
  minorUnits: number,
  money: MoneyFormat = DEFAULT_MONEY_FORMAT,
): string {
  const { digits, perMajor } = money;
  if (digits === 0) {
    return String(minorUnits);
  }

  // Minor units to the written scale, the inverse of what parseMoney does: 12
  // iraimbilanja at 5 per major become 24 tenths, which is written "2.4".
  const written = Math.round((minorUnits * 10 ** digits) / perMajor);
  const sign = written < 0 ? "-" : "";
  const places = String(Math.abs(written)).padStart(digits + 1, "0");
  return `${sign}${places.slice(0, -digits)}.${places.slice(-digits)}`;
}

/**
 * Formats a deadline as a relative hint: "in 2 hours", "in 2 Stunden".
 *
 * The unit is chosen by size rather than always being minutes, because "in 2880
 * minutes" is not a useful thing to tell somebody about lunch on Thursday.
 */
export function formatRelativeTime(
  locale: string,
  target: Date | string,
  now: Date = new Date(),
): string {
  const date = toDate(target);
  const unusable = unformattable(target, date);
  if (unusable !== null) {
    return unusable;
  }

  const seconds = Math.round((date.getTime() - now.getTime()) / 1000);
  const absolute = Math.abs(seconds);

  const [value, unit] = chooseUnit(seconds, absolute);
  return cached(`r:${locale}`, () =>
    new Intl.RelativeTimeFormat(locale, { numeric: "auto" }),
  ).format(value, unit);
}

function chooseUnit(seconds: number, absolute: number): [number, Intl.RelativeTimeFormatUnit] {
  const minute = 60;
  const hour = 60 * minute;
  const day = 24 * hour;
  const week = 7 * day;

  if (absolute < minute) {
    return [seconds, "second"];
  }
  if (absolute < hour) {
    return [Math.round(seconds / minute), "minute"];
  }
  if (absolute < day) {
    return [Math.round(seconds / hour), "hour"];
  }
  if (absolute < week) {
    return [Math.round(seconds / day), "day"];
  }
  return [Math.round(seconds / week), "week"];
}

/**
 * Parses an API timestamp.
 *
 * Everything crosses the API as RFC 3339 in UTC, and the browser converts to
 * the viewer's zone for display. No zone is ever shown: the specification
 * assumes the restaurant and everybody ordering share one.
 */
export function toDate(value: Date | string): Date {
  return value instanceof Date ? value : new Date(value);
}

/** Formats a Date back to what the API expects. */
export function toApiTimestamp(value: Date): string {
  return value.toISOString();
}

/** Empties the formatter cache. Only a test that switches locales needs this. */
export function resetFormatterCache(): void {
  cache.clear();
}

/**
 * Formats a byte count for a person: "4.2 MiB".
 *
 * Binary units, because that is what --max-image-size takes and what the
 * message compares against. Only the number follows the locale; MiB is a
 * symbol, not a word, and is not translated.
 */
export function formatBytes(locale: string, bytes: number): string {
  const kib = 1024;
  const mib = kib * kib;

  if (bytes >= mib) {
    return `${formatNumber(locale, Math.round((bytes / mib) * 10) / 10)} MiB`;
  }
  if (bytes >= kib) {
    return `${formatNumber(locale, Math.round(bytes / kib))} KiB`;
  }
  return `${formatNumber(locale, bytes)} B`;
}
