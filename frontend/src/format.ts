// Locale-aware formatting, all of it through the browser's Intl API.
//
// Everything takes the locale as an argument rather than reading a global, so a
// test can check the German rendering without switching anything, and a page
// can render one restaurant's prices while the interface is in another
// language. See docs/07_i18n.md.

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

/** Formats a date: `Sep 10, 2026` in English, `10.09.2026` in German. */
export function formatDate(locale: string, value: Date | string): string {
  const date = toDate(value);
  return cached(`d:${locale}`, () =>
    new Intl.DateTimeFormat(locale, { dateStyle: "medium" }),
  ).format(date);
}

/** Formats a time. The locale decides 12- or 24-hour, not this code. */
export function formatTime(locale: string, value: Date | string): string {
  const date = toDate(value);
  return cached(`t:${locale}`, () =>
    new Intl.DateTimeFormat(locale, { timeStyle: "short" }),
  ).format(date);
}

/** Formats a date and time together, for a deadline or a fulfilment time. */
export function formatDateTime(locale: string, value: Date | string): string {
  const date = toDate(value);
  return cached(`dt:${locale}`, () =>
    new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }),
  ).format(date);
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
 * The division by 10^minorUnit happens here, immediately before formatting, and
 * nowhere earlier. Every sum in the application is computed on the integers,
 * because a float that has been through three additions is not the number
 * anybody agreed to pay.
 */
export function formatMoney(
  locale: string,
  minorUnits: number,
  currency: string,
  minorUnit = 2,
): string {
  const formatter = cached(`m:${locale}:${currency}:${minorUnit}`, () => {
    try {
      return new Intl.NumberFormat(locale, {
        style: "currency",
        currency,
        minimumFractionDigits: minorUnit,
        maximumFractionDigits: minorUnit,
      });
    } catch {
      // An unknown currency code makes Intl throw. Falling back to a plain
      // number with the code appended is ugly but readable, and better than a
      // page that fails to render because a restaurant has an odd currency.
      return new Intl.NumberFormat(locale, {
        minimumFractionDigits: minorUnit,
        maximumFractionDigits: minorUnit,
      });
    }
  });

  const amount = minorUnits / 10 ** minorUnit;
  const formatted = formatter.format(amount);

  // The fallback path produced no currency, so add the code where a symbol
  // would have been.
  return formatter.resolvedOptions().style === "currency"
    ? formatted
    : `${formatted} ${currency}`;
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
