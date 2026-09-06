// "Open now" on a restaurant tile.

import { describe, expect, it } from "vitest";

import { isOpen, isoDay } from "../src/openinghours";

/** A local time on a known weekday. 2026-09-07 is a Monday. */
function monday(hours: number, minutes = 0): Date {
  return new Date(2026, 8, 7, hours, minutes);
}

function tuesday(hours: number, minutes = 0): Date {
  return new Date(2026, 8, 8, hours, minutes);
}

const lunch = { day_of_week: 1, start: "11:00", end: "14:00" };
const evening = { day_of_week: 1, start: "17:00", end: "02:00" };

describe("the weekday", () => {
  it("is ISO numbered: Monday is 1 and Sunday is 7", () => {
    expect(isoDay(monday(12))).toBe(1);
    expect(isoDay(new Date(2026, 8, 13, 12))).toBe(7);
  });
});

describe("whether a restaurant is open", () => {
  it("is open inside a period and closed outside it", () => {
    expect(isOpen([lunch], monday(12))).toBe(true);
    expect(isOpen([lunch], monday(15))).toBe(false);
  });

  it("closes at the closing time rather than a minute after", () => {
    expect(isOpen([lunch], monday(13, 59))).toBe(true);
    expect(isOpen([lunch], monday(14))).toBe(false);
  });

  it("is closed between the two halves of a lunch break", () => {
    expect(isOpen([lunch, evening], monday(15, 30))).toBe(false);
  });

  it("keeps a crossed midnight open into the next day", () => {
    // Monday 17:00 to 02:00 is still Monday's opening hours at one o'clock on
    // Tuesday morning, which is exactly when somebody looks.
    expect(isOpen([evening], monday(23))).toBe(true);
    expect(isOpen([evening], tuesday(1))).toBe(true);
    expect(isOpen([evening], tuesday(3))).toBe(false);
    // ...and not on Tuesday evening, which has no hours of its own.
    expect(isOpen([evening], tuesday(20))).toBe(false);
  });

  it("is closed when no hours are known at all", () => {
    expect(isOpen([], monday(12))).toBe(false);
  });

  it("ignores an entry it cannot read", () => {
    expect(isOpen([{ day_of_week: 1, start: "", end: "" }], monday(12))).toBe(false);
  });
});
