// The order page and the order overview.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { orderPage } from "../src/pages/order";
import { ordersPage } from "../src/pages/orders";
import { createOrderPage } from "../src/pages/ordercreate";
import type { App } from "../src/app";
import { mountApp, referenceStubs, settle, stubServer } from "./helpers";

const restaurant = {
  id: "r1",
  name: "Pinar Kebap",
  logo_image_id: null,
  currency_code: "CHF",
  min_order_value_cents: 2000,
  delivery_fee_cents: 350,
  notes: "",
  contacts: [
    {
      id: "c1",
      contact_type_id: "ct-phone",
      contact_type_code: "phone",
      render_as: "tel",
      value: "+41 44 123 45 67",
      label: "",
      sort_order: 10,
    },
  ],
  opening_hours: [{ id: "h1", day_of_week: 1, start: "11:00", end: "14:00", crosses_midnight: false }],
};

const menuItem = {
  id: "m1",
  category_id: "cat1",
  external_id: "12",
  name: "Döner Kebap",
  description: "Mit allem",
  image_id: null,
  price_cents: 950,
  available: true,
  tags: [{ id: "tag-vegan", code: "vegan", name: "vegan", sort_order: 10 }],
  allergens: [{ id: "al-gluten", code: "gluten", reference: "1", sort_order: 1 }],
  additives: [],
  modifications: [
    { id: "mod1", name: "Extra scharf", price_delta_cents: 0, sort_order: 10 },
    { id: "mod2", name: "Mit Käse", price_delta_cents: 100, sort_order: 20 },
  ],
};

/** An hour from now, so the order is open. */
function soon(hours: number): string {
  return new Date(Date.now() + hours * 3600 * 1000).toISOString();
}

function header(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "o1",
    title: "Pinar Kebap — 12:30",
    restaurant_id: "r1",
    restaurant_name: "Pinar Kebap",
    restaurant_logo_image_id: null,
    fulfilment: "pickup",
    fulfilment_at: soon(3),
    deadline_at: soon(2),
    status: "active",
    money_collector: "Jo",
    pickup_person: "",
    currency_code: "CHF",
    min_order_value_cents: 2000,
    delivery_fee_cents: 350,
    item_count: 1,
    created_at: soon(-24),
    updated_at: soon(-24),
    ...overrides,
  };
}

const orderItem = {
  id: "i1",
  user_id: "u2",
  user_name: "Alex",
  menu_item_id: "m1",
  quantity: 2,
  item_name: "Döner Kebap",
  unit_price_cents: 950,
  note: "ohne Zwiebeln",
  modifications: [
    { id: "om1", modification_id: "mod2", name: "Mit Käse", price_delta_cents: 100 },
  ],
  line_total_cents: 2100,
};

function detail(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    ...header(),
    creator_id: "u1",
    creator_name: "Jo",
    items: [orderItem],
    item_total_cents: 2100,
    grand_total_cents: 2450,
    below_minimum: false,
    ...overrides,
  };
}

function orderStubs(order: Record<string, unknown>): Record<string, unknown> {
  return {
    ...referenceStubs,
    "GET /orders/o1": order,
    "GET /restaurants/r1": restaurant,
    "GET /restaurants/r1/categories": { categories: [{ id: "cat1", name: "Kebap", sort_order: 10 }] },
    "GET /restaurants/r1/menu-items": { menu_items: [menuItem] },
    "GET /restaurants/r1/menu-items/m1": menuItem,
  };
}

/** An EventSource that does nothing, for the pages that are not testing one. */
const silentFactory = (): EventSource =>
  ({ addEventListener: () => {}, close: () => {}, readyState: 1 }) as unknown as EventSource;

function loggedIn(app: App, admin = false): void {
  app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: admin });
}

beforeEach(() => {
  document.body.replaceChildren();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the order overview", () => {
  it("shows the restaurant and the times, and nothing about the items", async () => {
    stubServer({ ...referenceStubs, "GET /orders": { orders: [header()] } });

    const app = mountApp(() => []);
    const rendered = await ordersPage(app);

    // The title is the tile's link, which is what makes the whole card
    // clickable without nesting one link inside another.
    const link = rendered.querySelector<HTMLAnchorElement>(".tile-link[href='/orders/o1']");
    expect(link?.textContent).toContain("Pinar");

    // The item count, the participant count, the total and the creator all used
    // to be here. A grid of tiles is for choosing between orders, not for
    // reading them, and every one of those is on the order itself.
    expect(rendered.textContent).not.toContain(app.t.t("order.items", { count: 1 }));
    // And an anonymous list names no user and carries no item data anyway
    // (ADR-0011).
    expect(rendered.textContent).not.toContain("Jo");
    expect(rendered.textContent).not.toContain("CHF");
  });

  it("still says nothing about items or people once logged in", async () => {
    stubServer({
      ...referenceStubs,
      "GET /orders": { orders: [{ ...header(), creator_id: "u1", creator_name: "Jo" }] },
      "GET /orders/o1": detail(),
    });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await ordersPage(app);

    expect(rendered.textContent).not.toContain("Jo");
    expect(rendered.textContent).not.toContain(app.t.t("order.participants", { count: 1 }));
    expect(rendered.textContent).not.toContain("24.50");
  });

  it("offers the summary to a participant, inside the tile", async () => {
    stubServer({
      ...referenceStubs,
      "GET /orders": { orders: [{ ...header(), creator_id: "u1", creator_name: "Jo" }] },
      "GET /orders/o1": detail(),
    });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await ordersPage(app);

    const summary = rendered.querySelector(".tile-actions a[href='/orders/o1/summary']");
    expect(summary).not.toBeNull();
  });

  it("keeps the summary from somebody who is not taking part", async () => {
    stubServer({
      ...referenceStubs,
      "GET /orders": { orders: [{ ...header(), creator_id: "u9", creator_name: "Somebody" }] },
      "GET /orders/o1": detail({ creator_id: "u9", items: [] }),
    });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await ordersPage(app);

    expect(rendered.querySelector("a[href='/orders/o1/summary']")).toBeNull();
  });

  it("gives the creator a pencil and a bin, and nobody else", async () => {
    const stubs = {
      ...referenceStubs,
      "GET /orders": { orders: [{ ...header(), creator_id: "u1", creator_name: "Jo" }] },
      "GET /orders/o1": detail(),
    };

    stubServer(stubs);
    const mine = mountApp(() => []);
    loggedIn(mine);
    const asCreator = await ordersPage(mine);
    expect(asCreator.querySelector(".tile-actions [aria-label]")).not.toBeNull();
    expect(asCreator.querySelectorAll(".tile-actions .button-icon").length).toBe(2);

    stubServer({
      ...stubs,
      "GET /orders": { orders: [{ ...header(), creator_id: "u9", creator_name: "Somebody" }] },
      "GET /orders/o1": detail({ creator_id: "u9" }),
    });
    const theirs = mountApp(() => []);
    loggedIn(theirs);
    const asStranger = await ordersPage(theirs);
    expect(asStranger.querySelectorAll(".tile-actions .button-icon").length).toBe(0);
  });

  it("offers the creator no pencil once the deadline has passed", async () => {
    stubServer({
      ...referenceStubs,
      "GET /orders": {
        orders: [{ ...header({ deadline_at: soon(-2) }), creator_id: "u1", creator_name: "Jo" }],
      },
      "GET /orders/o1": detail({ deadline_at: soon(-2) }),
    });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await ordersPage(app);

    // F6.6 makes an expired order read-only for its creator too, so the
    // controls that would change it are not offered at all.
    expect(rendered.querySelectorAll(".tile-actions .button-icon").length).toBe(0);
    // The summary stays: it is what somebody settling up afterwards wants.
    expect(rendered.querySelector("a[href='/orders/o1/summary']")).not.toBeNull();
  });

  it("fades an expired order and says so in words", async () => {
    stubServer({
      ...referenceStubs,
      "GET /orders": { orders: [header({ deadline_at: soon(-2), status: "expired" })] },
    });

    const app = mountApp(() => []);
    const rendered = await ordersPage(app);

    expect(rendered.querySelector(".tile-faded")).not.toBeNull();
    expect(rendered.textContent).toContain(app.t.t("order.status.expired"));
  });
});

describe("creating an order", () => {
  it("refuses a deadline that is not before the fulfilment time", async () => {
    const { calls } = stubServer({
      ...referenceStubs,
      "GET /restaurants": { restaurants: [restaurant] },
      "GET /restaurants/r1": restaurant,
      "POST /orders": { id: "o9" },
    });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await createOrderPage(app);
    await settle();

    const fulfilment = rendered.querySelector<HTMLInputElement>("input[name='fulfilment_at']");
    const deadline = rendered.querySelector<HTMLInputElement>("input[name='deadline_at']");
    if (!fulfilment || !deadline) {
      throw new Error("the form is missing its times");
    }

    fulfilment.value = "2026-09-10T12:00";
    deadline.value = "2026-09-10T13:00";
    deadline.dispatchEvent(new Event("input"));

    expect(rendered.textContent).toContain(app.t.t("order.deadline_before_fulfilment"));

    rendered.querySelector("form")?.dispatchEvent(new Event("submit"));
    await settle();
    expect(calls.some((call) => call.method === "POST")).toBe(false);
  });

  it("warns, but does not refuse, when the restaurant is closed then", async () => {
    stubServer({
      ...referenceStubs,
      "GET /restaurants": { restaurants: [restaurant] },
      "GET /restaurants/r1": restaurant,
      "POST /orders": { id: "o9" },
    });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await createOrderPage(app);
    await settle();

    const fulfilment = rendered.querySelector<HTMLInputElement>("input[name='fulfilment_at']");
    const deadline = rendered.querySelector<HTMLInputElement>("input[name='deadline_at']");
    if (!fulfilment || !deadline) {
      throw new Error("the form is missing its times");
    }

    // A Monday at 20:00. The restaurant is open 11:00 to 14:00.
    fulfilment.value = "2026-09-07T20:00";
    deadline.value = "2026-09-07T19:00";
    fulfilment.dispatchEvent(new Event("input"));

    expect(rendered.textContent).toContain(app.t.t("order.outside_hours"));
    // ...and the deadline rule is satisfied, so it is only a warning.
    expect(rendered.textContent).not.toContain(app.t.t("order.deadline_before_fulfilment"));
  });
});

describe("the order page", () => {
  it("shows an anonymous visitor the count and a way in, and no items", async () => {
    stubServer(orderStubs(header()));

    const app = mountApp(() => []);
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    expect(rendered.textContent).toContain(app.t.t("order.items_so_far", { count: 1 }));
    expect(rendered.textContent).toContain(app.t.t("order.anonymous_hint"));
    expect(rendered.querySelector("a[href='/account']")).not.toBeNull();
    // The item and the people are absent, not hidden: the API never sent them.
    expect(rendered.textContent).not.toContain("Alex");
    expect(rendered.textContent).not.toContain("ohne Zwiebeln");
  });

  it("groups the items by person and totals each of them", async () => {
    stubServer(orderStubs(detail()));

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    expect(rendered.querySelector(".person-name")?.textContent).toContain("Alex");
    expect(rendered.textContent).toContain("ohne Zwiebeln");
    expect(rendered.textContent).toContain("Mit Käse");
    // Two at 9.50 plus a franc of cheese each.
    expect(rendered.querySelector(".person-total")?.textContent).toContain("21.00");
    expect(rendered.textContent).toContain("24.50");
  });

  it("offers edit and delete only on a visitor's own items", async () => {
    stubServer(orderStubs(detail()));

    const app = mountApp(() => []);
    loggedIn(app); // u1, and the item belongs to u2
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    const itemButtons = [...(rendered.querySelector(".order-item")?.querySelectorAll("button") ?? [])];
    expect(itemButtons).toHaveLength(0);
  });

  it("lets the creator edit the order and offers the deletion", async () => {
    stubServer(orderStubs(detail()));

    const app = mountApp(() => []);
    loggedIn(app); // u1 is the creator
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    const labels = [...rendered.querySelectorAll("button")].map((control) => control.textContent);
    expect(labels).toContain(app.t.t("order.edit"));
    expect(labels).toContain(app.t.t("order.delete"));
  });

  it("takes every editing control away once the deadline has passed", async () => {
    // F6.6: read-only for everyone afterwards, the administrator included.
    stubServer(orderStubs(detail({ deadline_at: soon(-1), status: "expired" })));

    const app = mountApp(() => []);
    loggedIn(app, true);
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    const labels = [...rendered.querySelectorAll("button")].map((control) => control.textContent);
    expect(labels).not.toContain(app.t.t("order.edit"));
    expect(labels).not.toContain(app.t.t("item.add"));
    expect(rendered.textContent).toContain(app.t.t("order.closed_notice"));
  });

  it("adds an item with its options and a live line total", async () => {
    const { calls } = stubServer({ ...orderStubs(detail()), "POST /orders/o1/items": { id: "i9" } });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    const add = [...rendered.querySelectorAll("button")].find(
      (control) => control.textContent === app.t.t("item.add"),
    );
    add?.click();
    await settle();

    const modal = document.querySelector(".modal");
    expect(modal).not.toBeNull();

    const quantity = modal?.querySelector<HTMLInputElement>("input[type='number']");
    if (!quantity) {
      throw new Error("no quantity field");
    }
    quantity.value = "2";
    quantity.dispatchEvent(new Event("input"));

    // 2 × 9.50, before any option is ticked.
    expect(modal?.querySelector(".line-total")?.textContent).toContain("19.00");

    const cheese = [...(modal?.querySelectorAll("label.checkbox-row") ?? [])].find((row) =>
      row.textContent?.includes("Mit Käse"),
    );
    const box = cheese?.querySelector("input");
    if (box) {
      box.checked = true;
      box.dispatchEvent(new Event("change"));
    }
    expect(modal?.querySelector(".line-total")?.textContent).toContain("21.00");

    modal?.querySelector("form")?.dispatchEvent(new Event("submit"));
    await settle();

    const posted = calls.find((call) => call.method === "POST");
    expect(posted?.body).toMatchObject({
      menu_item_id: "m1",
      quantity: 2,
      modification_ids: ["mod2"],
    });
  });

  it("filters the menu by tag and by excluded allergen", async () => {
    const plain = {
      ...menuItem,
      id: "m2",
      name: "Falafel",
      tags: [],
      allergens: [],
      modifications: [],
    };
    stubServer({
      ...orderStubs(detail()),
      "GET /restaurants/r1/menu-items": { menu_items: [menuItem, plain] },
    });

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    const tick = (label: string): void => {
      const row = [...rendered.querySelectorAll("label.checkbox-row")].find(
        (entry) => entry.textContent === label,
      );
      const box = row?.querySelector("input");
      if (box) {
        box.checked = true;
        box.dispatchEvent(new Event("change"));
      }
    };

    tick(app.t.t("tag.vegan"));
    expect(rendered.textContent).toContain("Döner Kebap");
    expect(rendered.textContent).not.toContain("Falafel");

    // Clearing brings both back, and excluding gluten removes the doner.
    [...rendered.querySelectorAll("button")]
      .find((control) => control.textContent === app.t.t("menu.filter.clear"))
      ?.click();
    tick(app.t.t("allergen.gluten"));

    expect(rendered.textContent).toContain("Falafel");
    expect(rendered.querySelector(".menu-item strong")?.textContent).toBe("Falafel");
  });

  it("collapses a category and says so to a screen reader", async () => {
    stubServer(orderStubs(detail()));

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    const toggle = rendered.querySelector<HTMLButtonElement>(".category-toggle");
    expect(toggle?.getAttribute("aria-expanded")).toBe("true");

    toggle?.click();
    await settle();

    const after = rendered.querySelector<HTMLButtonElement>(".category-toggle");
    expect(after?.getAttribute("aria-expanded")).toBe("false");
    expect(rendered.querySelector<HTMLElement>(".menu-items")?.hidden).toBe(true);
  });

  it("offers a way to add a dish the menu is missing", async () => {
    stubServer(orderStubs(detail()));

    const app = mountApp(() => []);
    loggedIn(app);
    const rendered = await orderPage(app, "o1", { factory: silentFactory });
    await settle();

    const labels = [...rendered.querySelectorAll("button")].map((control) => control.textContent);
    expect(labels).toContain(app.t.t("order.add_missing_item"));
  });
});

describe("live updates on the order page", () => {
  /** An EventSource whose events the test can deliver by hand. */
  function controllable(): {
    factory: (url: string) => EventSource;
    fire: (name: string, data: unknown) => void;
    opened: () => void;
  } {
    const listeners = new Map<string, ((event: Event) => void)[]>();
    const factory = (): EventSource =>
      ({
        readyState: 1,
        addEventListener: (name: string, listener: (event: Event) => void) => {
          listeners.set(name, [...(listeners.get(name) ?? []), listener]);
        },
        close: () => {},
      }) as unknown as EventSource;

    const deliver = (name: string, event: Event): void => {
      for (const listener of listeners.get(name) ?? []) {
        listener(event);
      }
    };

    return {
      factory,
      opened: () => deliver("open", new Event("open")),
      fire: (name, data) =>
        deliver(name, { data: JSON.stringify(data) } as unknown as Event),
    };
  }

  it("re-fetches the order when somebody else adds an item", async () => {
    const stubs = orderStubs(detail());
    stubServer(stubs);

    const app = mountApp(() => []);
    loggedIn(app);
    const stream = controllable();
    const rendered = await orderPage(app, "o1", { factory: stream.factory });
    await settle();

    // The next fetch answers with a second item, as another browser's addition
    // would.
    const second = { ...orderItem, id: "i2", user_id: "u3", user_name: "Sam", item_name: "Falafel" };
    stubs["GET /orders/o1"] = detail({ items: [orderItem, second], item_count: 2 });
    stubServer(stubs);

    stream.opened();
    stream.fire("item.created", { id: "i2" });
    await settle(20);

    expect(rendered.textContent).toContain("Sam");
    expect(document.getElementById("live-region")?.textContent).toBe(app.t.t("order.live.changed"));
  });

  it("switches to read-only when the deadline passes while the page is open", async () => {
    const stubs = orderStubs(detail());
    stubServer(stubs);

    const app = mountApp(() => []);
    loggedIn(app);
    const stream = controllable();
    const rendered = await orderPage(app, "o1", { factory: stream.factory });
    await settle();

    expect(
      [...rendered.querySelectorAll("button")].map((control) => control.textContent),
    ).toContain(app.t.t("item.add"));

    // F7.3: the server announces the expiry, and the page changes state without
    // a reload.
    stubs["GET /orders/o1"] = detail({ deadline_at: soon(-0.01), status: "expired" });
    stubServer(stubs);
    stream.opened();
    stream.fire("order.expired", { id: "o1" });
    await settle(20);

    expect(rendered.textContent).toContain(app.t.t("order.closed_notice"));
    expect(
      [...rendered.querySelectorAll("button")].map((control) => control.textContent),
    ).not.toContain(app.t.t("item.add"));
  });

  it("says so when the order is deleted underneath the page", async () => {
    stubServer(orderStubs(detail()));

    const app = mountApp(() => []);
    loggedIn(app);
    const stream = controllable();
    const rendered = await orderPage(app, "o1", { factory: stream.factory });
    await settle();

    stream.opened();
    stream.fire("order.deleted", { id: "o1" });
    await settle();

    expect(rendered.textContent).toContain(app.t.t("order.deleted"));
  });
});
