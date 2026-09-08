// The Intl formatting helpers.
//
// The assertions look for the parts that matter -- the separator, the currency,
// the order of the two -- rather than for a whole string, because the exact
// spacing CLDR uses between an amount and its currency changes between ICU
// versions and is not something this application decides.

import { beforeEach, describe, expect, it } from "vitest";

import {
  formatBytes,
  formatClockTime,
  formatDate,
  formatDateTime,
  formatMoney,
  formatNumber,
  formatRelativeTime,
  formatWeekday,
  moneyInputValue,
  parseMoney,
  resetFormatterCache,
  toApiTimestamp,
} from "../src/format";

beforeEach(() => {
  resetFormatterCache();
});

describe("money", () => {
  it("formats a CHF restaurant in German", () => {
    // The sprint's exit criterion. The currency comes from the restaurant and
    // the number formatting from the interface language, so this is CHF with a
    // German decimal comma.
    const formatted = formatMoney("de", 650, "CHF");
    expect(formatted).toContain("6,50");
    expect(formatted).toContain("CHF");
  });

  it("formats the same amount in English", () => {
    const formatted = formatMoney("en", 650, "CHF");
    expect(formatted).toContain("6.50");
    expect(formatted).toContain("CHF");
  });

  it("keeps the restaurant's currency whatever the language", () => {
    expect(formatMoney("de", 650, "EUR")).toContain("€");
    expect(formatMoney("en", 650, "EUR")).toContain("€");
  });

  it("divides by the minor unit and not before", () => {
    expect(formatMoney("en", 1, "EUR")).toContain("0.01");
    expect(formatMoney("en", 123456, "EUR")).toContain("1,234.56");
  });

  it("handles a currency with no minor unit", () => {
    const formatted = formatMoney("en", 1200, "JPY", 0);
    expect(formatted).toContain("1,200");
    expect(formatted).not.toContain(".");
  });

  it("still renders when Intl refuses the currency", () => {
    // A malformed code makes Intl throw. A page that failed to render because
    // a restaurant had an odd currency would be a worse answer than this one.
    const formatted = formatMoney("en", 650, "EU");
    expect(formatted).toContain("6.50");
    expect(formatted).toContain("EU");
  });
});

describe("dates and times", () => {
  const when = "2026-09-10T10:30:00Z";

  it("formats a date the way the locale does", () => {
    expect(formatDate("de", when)).toMatch(/10\.09\.2026/u);
    expect(formatDate("en", when)).toMatch(/Sep 10, 2026/u);
  });

  it("formats a date and time together", () => {
    expect(formatDateTime("de", when)).toMatch(/10\.09\.2026/u);
  });

  it("names weekdays in the interface language", () => {
    expect(formatWeekday("en", 1)).toBe("Monday");
    expect(formatWeekday("de", 1)).toBe("Montag");
    expect(formatWeekday("de", 7)).toBe("Sonntag");
  });

  it("shows an opening-hours time as stored, without a time zone shift", () => {
    // A restaurant that opens at 11:00 opens at 11:00 wherever the viewer is:
    // opening hours are wall-clock values with no date (docs/07_i18n.md).
    expect(formatClockTime("de", "11:00")).toBe("11:00");
    expect(formatClockTime("en", "23:30")).toMatch(/11:30\s*PM/u);
  });

  it("round-trips a timestamp back to what the API wants", () => {
    expect(toApiTimestamp(new Date(when))).toBe("2026-09-10T10:30:00.000Z");
  });
});

describe("relative times", () => {
  const now = new Date("2026-09-10T10:00:00Z");

  it("chooses a unit a person would use", () => {
    expect(formatRelativeTime("en", "2026-09-10T12:00:00Z", now)).toBe("in 2 hours");
    expect(formatRelativeTime("de", "2026-09-10T12:00:00Z", now)).toBe("in 2 Stunden");
    expect(formatRelativeTime("en", "2026-09-10T10:30:00Z", now)).toBe("in 30 minutes");
    expect(formatRelativeTime("en", "2026-09-13T10:00:00Z", now)).toBe("in 3 days");
    expect(formatRelativeTime("en", "2026-10-01T10:00:00Z", now)).toBe("in 3 weeks");
  });

  it("looks backwards for a deadline that has passed", () => {
    expect(formatRelativeTime("en", "2026-09-10T08:00:00Z", now)).toBe("2 hours ago");
  });
});

describe("numbers", () => {
  it("follows the locale's separators", () => {
    expect(formatNumber("de", 1234.5)).toBe("1.234,5");
    expect(formatNumber("en", 1234.5)).toBe("1,234.5");
  });
});

describe("reading an amount somebody typed", () => {
  it("takes either separator, whatever the language", () => {
    // A German numeric keypad produces a comma. Nobody should have to think
    // about which one a price field wants.
    expect(parseMoney("6.50")).toBe(650);
    expect(parseMoney("6,50")).toBe(650);
  });

  it("is exact where a float is not", () => {
    // Math.round(parseFloat("19.99") * 100) lands on 1998.9999999999998.
    expect(parseMoney("19.99")).toBe(1999);
    expect(parseMoney("0.07")).toBe(7);
    expect(parseMoney("1234.56")).toBe(123456);
  });

  it("pads a short fraction and cuts a long one", () => {
    expect(parseMoney("6.5")).toBe(650);
    expect(parseMoney("6")).toBe(600);
    // Cut, not rounded: a third decimal is a slip, and rounding it up would
    // charge a cent nobody agreed to.
    expect(parseMoney("6.509")).toBe(650);
  });

  it("follows the currency's minor unit", () => {
    expect(parseMoney("1200", 0)).toBe(1200);
    expect(parseMoney("1.234", 3)).toBe(1234);
  });

  it("says so when it is not a number", () => {
    expect(parseMoney("")).toBeNull();
    expect(parseMoney("   ")).toBeNull();
    expect(parseMoney("free")).toBeNull();
  });

  it("round-trips through the input value", () => {
    expect(moneyInputValue(650)).toBe("6.50");
    expect(moneyInputValue(7)).toBe("0.07");
    expect(moneyInputValue(1200, 0)).toBe("1200");
    expect(parseMoney(moneyInputValue(123456))).toBe(123456);
  });
});

describe("byte sizes", () => {
  it("uses binary units, as --max-image-size does", () => {
    expect(formatBytes("en", 5 * 1024 * 1024)).toBe("5 MiB");
    expect(formatBytes("en", 1536)).toBe("2 KiB");
    expect(formatBytes("en", 512)).toBe("512 B");
  });

  it("formats the number in the interface language", () => {
    expect(formatBytes("de", 1024 * 1024 * 2.5)).toBe("2,5 MiB");
  });
});
