// The availability rules, as a person edits them.
//
// What the rules *mean* is the server's business and is tested there; these are
// about the page saying it clearly and sending what it was asked to send.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { describeFilter, filterPicker } from "../src/pages/availability";
import { restaurantPage } from "../src/pages/restaurant";
import { mountApp, referenceStubs, settle, stubServer } from "./helpers";

const restaurant = {
  id: "r1",
  name: "Pinar Kebap",
  logo_image_id: null,
  currency_code: "EUR",
  min_order_value_cents: null,
  delivery_fee_cents: null,
  notes: "",
  contacts: [],
  opening_hours: [],
};

const pasta = {
  id: "f1",
  restaurant_id: "r1",
  name: "Fri-Sun after 5",
  on_date: null,
  weekdays: [5, 6, 7],
  start_time: "17:00",
  end_time: "22:00",
  sort_order: 0,
};

const christmas = {
  id: "f2",
  restaurant_id: "r1",
  name: "Weihnachtsessen",
  on_date: "2026-12-24",
  weekdays: [],
  start_time: null,
  end_time: null,
  sort_order: 10,
};

function restaurantStubs(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    ...referenceStubs,
    "GET /restaurants/r1": restaurant,
    "GET /restaurants/r1/categories": { categories: [] },
    "GET /restaurants/r1/menu-items": { menu_items: [] },
    "GET /restaurants/r1/availability": { availability: [pasta, christmas] },
    ...overrides,
  };
}

beforeEach(() => {
  document.body.replaceChildren();
  history.replaceState(null, "", "/");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/*
 * A list of names is a list of things somebody has to remember the meaning of.
 * "Fri-Sun after 5" is only a promise that the rule matches its name, and the
 * one place that promise can be checked is beside it.
 */
describe("what a rule says", () => {
  it("spells out the weekdays and the window", () => {
    const app = mountApp(() => []);
    const described = describeFilter(app, pasta);

    expect(described).toContain("17:00");
    expect(described).toContain("22:00");
    // The weekdays are named in the interface language, abbreviated.
    expect(described.length).toBeGreaterThan("17:00–22:00".length);
  });

  it("shows a date on its own", () => {
    const app = mountApp(() => []);
    expect(describeFilter(app, christmas)).toContain("2026-12-24");
  });

  it("says so when a rule restricts nothing it can name", () => {
    const app = mountApp(() => []);
    const empty = { ...pasta, weekdays: [], start_time: null, end_time: null };
    expect(describeFilter(app, empty)).toBe(app.t.t("availability.always"));
  });
});

describe("the picker that attaches rules", () => {
  it("ticks what is already attached and reports what is chosen", () => {
    const app = mountApp(() => []);
    const picker = filterPicker(app, [pasta, christmas], [christmas.id]);

    const boxes = [...picker.element.querySelectorAll<HTMLInputElement>("input")];
    expect(boxes.length).toBe(2);
    expect(boxes[0]?.checked).toBe(false);
    expect(boxes[1]?.checked).toBe(true);

    expect(picker.selected()).toEqual([christmas.id]);

    boxes[0]!.checked = true;
    expect(picker.selected()).toEqual([pasta.id, christmas.id]);
  });

  // Somebody who has defined no rules gets a sentence rather than an empty box
  // with a legend, which reads as a list that failed to load.
  it("explains itself when the restaurant has no rules", () => {
    const app = mountApp(() => []);
    const picker = filterPicker(app, [], []);

    expect(picker.element.textContent).toContain(app.t.t("availability.none_defined"));
    expect(picker.selected()).toEqual([]);
  });
});

describe("the availability tab", () => {
  it("lists the restaurant's rules with what each one says", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    const panel = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")].at(-1);
    expect(panel?.textContent).toContain("Fri-Sun after 5");
    expect(panel?.textContent).toContain("Weihnachtsessen");
    expect(panel?.textContent).toContain("2026-12-24");
  });

  it("offers the delete only to the administrator", async () => {
    stubServer(restaurantStubs());

    const ordinary = mountApp(() => []);
    ordinary.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const asUser = await restaurantPage(ordinary, "r1");
    await settle();
    const userPanel = [...asUser.querySelectorAll<HTMLElement>("[role='tabpanel']")].at(-1);
    expect(userPanel?.textContent).not.toContain(ordinary.t.t("action.delete"));

    stubServer(restaurantStubs());
    const admin = mountApp(() => []);
    admin.session.set({ id: "u2", name: "root", display_name: "root", is_admin: true });
    const asAdmin = await restaurantPage(admin, "r1");
    await settle();
    const adminPanel = [...asAdmin.querySelectorAll<HTMLElement>("[role='tabpanel']")].at(-1);
    expect(adminPanel?.textContent).toContain(admin.t.t("action.delete"));
  });

  it("says so when there are none, rather than showing an empty list", async () => {
    stubServer(restaurantStubs({ "GET /restaurants/r1/availability": { availability: [] } }));

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    const panel = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")].at(-1);
    expect(panel?.textContent).toContain(app.t.t("availability.none"));
  });
});
