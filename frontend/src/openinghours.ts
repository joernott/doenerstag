// Is the restaurant open right now?
//
// Opening hours are wall-clock values with no date and no time zone
// (docs/07_i18n.md): a restaurant that opens at 11:00 opens at 11:00, and the
// specification assumes the restaurant and everybody ordering share a zone. So
// "now" is the viewer's own clock, compared against the stored strings.

export interface OpeningPeriod {
  day_of_week: number;
  start: string;
  end: string;
}

/** Minutes since midnight, or null for anything that is not HH:MM. */
function minutes(value: string): number | null {
  const match = /^(\d{1,2}):(\d{2})/u.exec(value);
  if (!match) {
    return null;
  }
  return Number.parseInt(match[1] ?? "0", 10) * 60 + Number.parseInt(match[2] ?? "0", 10);
}

/** The ISO weekday of a date: 1 is Monday, 7 is Sunday. */
export function isoDay(date: Date): number {
  return date.getDay() === 0 ? 7 : date.getDay();
}

/**
 * Whether any period covers the given moment.
 *
 * A period whose end is not after its start crosses midnight, and it is
 * genuinely two intervals: the evening of its own day, and the small hours of
 * the next one. Both are checked, which is why Tuesday at 01:00 counts as
 * Monday's opening hours still running.
 */
export function isOpen(periods: readonly OpeningPeriod[], now: Date = new Date()): boolean {
  const today = isoDay(now);
  const yesterday = today === 1 ? 7 : today - 1;
  const clock = now.getHours() * 60 + now.getMinutes();

  for (const period of periods) {
    const start = minutes(period.start);
    const end = minutes(period.end);
    if (start === null || end === null) {
      continue;
    }

    if (end > start) {
      if (period.day_of_week === today && clock >= start && clock < end) {
        return true;
      }
      continue;
    }

    // Crosses midnight: the evening part belongs to its own day, the morning
    // part to the day after.
    if (period.day_of_week === today && clock >= start) {
      return true;
    }
    if (period.day_of_week === yesterday && clock < end) {
      return true;
    }
  }

  return false;
}
