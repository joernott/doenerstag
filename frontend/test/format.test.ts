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
    const formatted = formatMoney("en", 1200, "JPY", { digits: 0, perMajor: 1 });
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
    expect(parseMoney("1200", { digits: 0, perMajor: 1 })).toBe(1200);
    expect(parseMoney("1.234", { digits: 3, perMajor: 1000 })).toBe(1234);
  });

  it("says so when it is not a number", () => {
    expect(parseMoney("")).toBeNull();
    expect(parseMoney("   ")).toBeNull();
    expect(parseMoney("free")).toBeNull();
  });

  it("round-trips through the input value", () => {
    expect(moneyInputValue(650)).toBe("6.50");
    expect(moneyInputValue(7)).toBe("0.07");
    expect(moneyInputValue(1200, { digits: 0, perMajor: 1 })).toBe("1200");
    expect(parseMoney(moneyInputValue(123456))).toBe(123456);
  });
});


/*
 * The Malagasy ariary and the Mauritanian ouguiya, the two currencies still in
 * use that do not divide into a power of ten.
 *
 * One ariary is five iraimbilanja. Written with one decimal place, the amounts
 * that can exist are .0, .2, .4, .6 and .8 -- and the divisor is five, not ten.
 * Deriving it from the number of decimal places, which is what this code did
 * until it was told otherwise, halves every amount: ten iraimbilanja become one
 * ariary instead of two.
 */
describe("a currency that does not divide by ten", () => {
  const ariary = { digits: 1, perMajor: 5 };

  it("formats minor units by what the currency divides into", () => {
    // The case in the report: ten iraimbilanja are two ariary.
    expect(formatMoney("en", 10, "MGA", ariary)).toContain("2.0");
    expect(formatMoney("en", 5, "MGA", ariary)).toContain("1.0");
    expect(formatMoney("en", 12, "MGA", ariary)).toContain("2.4");
    expect(formatMoney("en", 1, "MGA", ariary)).toContain("0.2");
  });

  it("reads a typed amount back into iraimbilanja", () => {
    expect(parseMoney("2", ariary)).toBe(10);
    expect(parseMoney("2.4", ariary)).toBe(12);
    expect(parseMoney("0.2", ariary)).toBe(1);
    expect(parseMoney("0.8", ariary)).toBe(4);
  });

  // An amount between two coins is rounded to one, because there is no such
  // thing as 2.3 ariary: the nearest payable amount is 2.4.
  it("rounds an amount that falls between two coins", () => {
    expect(parseMoney("2.3", ariary)).toBe(12);
    expect(parseMoney("2.1", ariary)).toBe(11);
  });

  it("round-trips through the input field", () => {
    for (const minorUnits of [0, 1, 4, 5, 10, 12, 137]) {
      expect(parseMoney(moneyInputValue(minorUnits, ariary), ariary)).toBe(minorUnits);
    }
  });

  // The half that was already right stays right: a decimal currency divides by
  // ten to the power of its places, which is what the ratio says for it too.
  it("leaves the decimal currencies exactly as they were", () => {
    const euro = { digits: 2, perMajor: 100 };
    expect(formatMoney("en", 1999, "EUR", euro)).toBe(formatMoney("en", 1999, "EUR"));
    expect(parseMoney("19.99", euro)).toBe(1999);
    expect(moneyInputValue(1999, euro)).toBe("19.99");
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

describe("a value that is not a timestamp", () => {
  it("is shown as it stands rather than throwing", () => {
    // A development build reports its build date as "unknown". Intl throws on
    // an invalid date, and a throw inside a render takes the whole page with
    // it -- which is exactly what the version page did until this existed.
    expect(formatDate("en", "unknown")).toBe("unknown");
    expect(formatDateTime("de", "unknown")).toBe("unknown");
    expect(formatRelativeTime("en", "not a date")).toBe("not a date");
  });

  it("still formats a real one", () => {
    expect(formatDateTime("en", "2026-09-10T10:30:00Z")).toContain("2026");
  });
});
