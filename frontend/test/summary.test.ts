// The summary page.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { summaryPage } from "../src/pages/summary";
import { fails, mountApp, referenceStubs, settle, stubServer } from "./helpers";

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
    {
      id: "c2",
      contact_type_id: "ct-address",
      contact_type_code: "address",
      render_as: "address",
      value: "Bahnhofstrasse 1, 8001 Zürich",
      label: "",
      sort_order: 20,
    },
  ],
  opening_hours: [],
};

const order = {
  id: "o1",
  title: "Pinar Kebap — 12:30",
  restaurant_id: "r1",
  restaurant_name: "Pinar Kebap",
  restaurant_logo_image_id: null,
  fulfilment: "pickup",
  fulfilment_at: "2026-09-10T10:30:00Z",
  deadline_at: "2026-09-10T09:30:00Z",
  status: "expired",
  money_collector: "",
  pickup_person: "",
  currency_code: "CHF",
  min_order_value_cents: 2000,
  delivery_fee_cents: 350,
  item_count: 4,
};

const summary = {
  order_id: "o1",
  title: "Pinar Kebap — 12:30",
  currency_code: "CHF",
  aggregated: [
    {
      item_name: "Döner Kebap",
      external_id: "12",
      modifications: ["Mit Käse"],
      note: null,
      count: 3,
      unit_price_cents: 1050,
      total_cents: 3150,
    },
    {
      item_name: "Falafel",
      external_id: "",
      modifications: [],
      note: "ohne Zwiebeln",
      count: 1,
      unit_price_cents: 850,
      total_cents: 850,
    },
  ],
  per_person: [
    {
      user_id: "u1",
      display_name: "Jo",
      items: [
        {
          id: "i1",
          quantity: 2,
          item_name: "Döner Kebap",
          note: "",
          modifications: [{ name: "Mit Käse" }],
          line_total_cents: 2100,
          paid: false,
        },
      ],
      total_cents: 2100,
      paid_cents: 0,
    },
    {
      user_id: "u2",
      display_name: "deleted user",
      items: [
        {
          id: "i2",
          quantity: 1,
          item_name: "Falafel",
          note: "ohne Zwiebeln",
          modifications: [],
          line_total_cents: 850,
          paid: false,
        },
      ],
      total_cents: 850,
      paid_cents: 0,
    },
  ],
  item_total_cents: 4000,
  delivery_fee_cents: 350,
  grand_total_cents: 4350,
  min_order_value_cents: 2000,
  below_minimum: false,
  plain_text: "Pinar Kebap\n3 × Döner Kebap (Mit Käse)\n1 × Falafel\nTotal: CHF 43.50\n",
};

function summaryStubs(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    ...referenceStubs,
    "GET /orders/o1/summary": summary,
    "GET /orders/o1": order,
    "GET /restaurants/r1": restaurant,
    ...overrides,
  };
}

beforeEach(() => {
  document.body.replaceChildren();
  // A page test that moves the address moves it for the whole file otherwise,
  // and pages read the address when they build their login links.
  history.replaceState(null, "", "/");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the summary", () => {
  it("has the four sections in the documented order", async () => {
    stubServer(summaryStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const titles = [...rendered.querySelectorAll(".card-title")].map((entry) => entry.textContent);
    expect(titles).toEqual([
      summary.title,
      app.t.t("summary.what_to_order"),
      app.t.t("summary.who_owes_what"),
      app.t.t("order.totals"),
      app.t.t("summary.actions"),
    ]);
  });

  it("puts the restaurant's number in the header as something to dial", async () => {
    stubServer(summaryStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const phone = rendered.querySelector("a.phone");
    // The punctuation people write a number with is not valid in a tel: URI,
    // so the href is the digits and the visible text is what was typed.
    expect(phone?.getAttribute("href")).toBe("tel:+41441234567");
    expect(phone?.textContent).toBe("+41 44 123 45 67");
  });

  // The page for the person going to fetch the food, which had every detail
  // except where to go.
  it("puts the address in the header, as a link to a map", async () => {
    stubServer(summaryStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const address = rendered.querySelector(".summary-addresses a");
    expect(address?.textContent).toBe("Bahnhofstrasse 1, 8001 Zürich");
    expect(address?.getAttribute("href")).toContain("google.com/maps");
    expect(address?.getAttribute("href")).toContain(encodeURIComponent("Bahnhofstrasse 1"));
  });

  it("aggregates the order into the lines somebody reads out", async () => {
    stubServer(summaryStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const lines = [...rendered.querySelectorAll(".dictation-line")];
    expect(lines).toHaveLength(2);
    expect(lines[0]?.querySelector(".dictation-count")?.textContent).toBe("3×");
    // The item number is a dialling aid: "number 12, three times".
    expect(lines[0]?.querySelector(".menu-number")?.textContent).toBe("12");
    expect(lines[0]?.textContent).toContain("Mit Käse");
    expect(lines[1]?.textContent).toContain("ohne Zwiebeln");
  });

  it("shows each person's total, deleted accounts included", async () => {
    stubServer(summaryStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const people = [...rendered.querySelectorAll(".person-name")].map((entry) => entry.textContent);
    expect(people[0]).toContain("Jo");
    expect(people[0]).toContain("21.00");
    // Whatever the API calls a deleted account is what the page shows; it does
    // not invent a label of its own.
    expect(people[1]).toContain("deleted user");
  });

  it("totals the order and names the currency the restaurant charges in", async () => {
    stubServer(summaryStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const totals = rendered.querySelector(".totals")?.textContent ?? "";
    expect(totals).toContain("40.00");
    expect(totals).toContain("3.50");
    expect(totals).toContain("43.50");
    expect(totals).toContain("CHF");
  });

  it("warns when the order is below the restaurant's minimum", async () => {
    stubServer(summaryStubs({ "GET /orders/o1/summary": { ...summary, below_minimum: true } }));

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const notice = rendered.querySelector(".notice-warning");
    expect(notice?.textContent).toContain(app.t.t("order.below_minimum"));
    // And says what the minimum is, which is the number somebody needs.
    expect(notice?.textContent).toContain("20.00");
  });

  it("copies the API's own text rather than a second rendering", async () => {
    stubServer(summaryStubs());
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const copy = [...rendered.querySelectorAll("button")].find(
      (control) => control.textContent === app.t.t("summary.copy"),
    );
    copy?.click();
    await settle();

    expect(writeText).toHaveBeenCalledWith(summary.plain_text);
    expect(rendered.querySelector(".status")?.textContent).toBe(app.t.t("action.copied"));
  });

  it("offers the text to select when the clipboard refuses", async () => {
    stubServer(summaryStubs());
    vi.stubGlobal("navigator", {
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
    });

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    [...rendered.querySelectorAll("button")]
      .find((control) => control.textContent === app.t.t("summary.copy"))
      ?.click();
    await settle();

    const fallback = rendered.querySelector<HTMLTextAreaElement>("textarea");
    expect(fallback?.hidden).toBe(false);
    expect(fallback?.value).toBe(summary.plain_text);
  });
});

describe("somebody who may not read it", () => {
  it("is told why, and offered the way back", async () => {
    stubServer(
      summaryStubs({
        "GET /orders/o1/summary": fails(403, {
          error: { code: 3004, message: "only participants may see this summary" },
        }),
      }),
    );

    const app = mountApp(() => []);
    app.session.set({ id: "u9", name: "sam", display_name: "Sam", is_admin: false });
    const rendered = await summaryPage(app, "o1");

    expect(rendered.textContent).toContain(app.t.t("summary.participants_only"));
    expect(rendered.querySelector("a[href='/orders/o1']")).not.toBeNull();
    // Already logged in, so there is nothing to log in to. Matched by prefix
    // because the link, when there is one, carries a return address.
    expect(rendered.querySelector("a[href^='/account']")).toBeNull();
  });

  it("is offered the login page when anonymous, because logging in may help", async () => {
    // The link comes back here, so the page has to be here to begin with.
    history.replaceState(null, "", "/orders/o1/summary");
    stubServer(
      summaryStubs({
        "GET /orders/o1/summary": fails(401, {
          error: { code: 2000, message: "not authenticated" },
        }),
      }),
    );

    const app = mountApp(() => []);
    const rendered = await summaryPage(app, "o1");

    expect(rendered.textContent).toContain(app.t.t("summary.participants_only"));
    expect(
      rendered.querySelector("a[href='/account?next=%2Forders%2Fo1%2Fsummary']"),
    ).not.toBeNull();
  });
});

describe("when the browser has no clipboard", () => {
  it("offers the text to select instead of doing nothing", async () => {
    stubServer(summaryStubs());
    // Outside a secure context there is no clipboard object at all, which a
    // development server with a self-signed certificate produces. The button
    // used to do nothing whatsoever in that case.
    vi.stubGlobal("navigator", {});

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    [...rendered.querySelectorAll("button")]
      .find((control) => control.textContent === app.t.t("summary.copy"))
      ?.click();

    const fallback = rendered.querySelector<HTMLTextAreaElement>("textarea");
    expect(fallback?.hidden).toBe(false);
    expect(fallback?.value).toBe(summary.plain_text);
    expect(rendered.querySelector(".status")?.textContent).toBe(app.t.t("summary.copy_failed"));
  });
});

/*
 * The tick beside a price.
 *
 * Not a payment -- the application handles none. It is the person with the
 * money marking a line off a list, which is what they were doing on paper.
 */
describe("recording that a line has been settled", () => {
  function summaryWithPaidItem(): Record<string, unknown> {
    const paid = structuredClone(summary) as typeof summary;
    paid.per_person[0]!.items[0]!.paid = true;
    paid.per_person[0]!.total_cents = 0;
    paid.per_person[0]!.paid_cents = 2100;
    return paid as unknown as Record<string, unknown>;
  }

  it("offers the owner a live checkbox and shows it ticked", async () => {
    stubServer(summaryStubs({ "GET /orders/o1/summary": summaryWithPaidItem() }));

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const boxes = [...rendered.querySelectorAll<HTMLInputElement>(".paid-box input")];
    expect(boxes.length).toBe(2);
    expect(boxes[0]?.checked).toBe(true);
    expect(boxes[0]?.disabled).toBe(false);

    // Somebody else's line: readable, not tickable, because this visitor is
    // neither its owner nor the creator nor the collector.
    expect(boxes[1]?.disabled).toBe(true);
  });

  it("leaves a settled line out of what that person owes", async () => {
    stubServer(summaryStubs({ "GET /orders/o1/summary": summaryWithPaidItem() }));

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const group = rendered.querySelector(".person-group");
    expect(group?.querySelector(".person-total")?.textContent).toContain("0.00");
    // And says what was settled, so a zero cannot be read as "ordered nothing".
    expect(group?.querySelector(".person-paid")?.textContent).toContain("21.00");
  });

  it("tells the server when the box is ticked", async () => {
    const stubs = stubServer(
      summaryStubs({ "PUT /orders/o1/items/i1/paid": { ok: true } }),
    );

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const box = rendered.querySelector<HTMLInputElement>(".paid-box input");
    box!.checked = true;
    box?.dispatchEvent(new Event("change"));
    await settle();

    const call = stubs.calls.find((entry) => entry.path === "/orders/o1/items/i1/paid");
    expect(call?.method).toBe("PUT");
    expect(call?.body).toEqual({ paid: true });
  });

  // The creator and the money collector may tick anybody's line, because they
  // are the two people who would know.
  it("lets the money collector tick a line that is not theirs", async () => {
    stubServer(
      summaryStubs({
        "GET /orders/o1": { ...order, creator_id: "u9", money_collector_id: "u7" },
      }),
    );

    const app = mountApp(() => []);
    app.session.set({ id: "u7", name: "kasse", display_name: "Kasse", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const boxes = [...rendered.querySelectorAll<HTMLInputElement>(".paid-box input")];
    expect(boxes.every((box) => !box.disabled)).toBe(true);
  });

  it("offers a bystander no way to tick anything", async () => {
    stubServer(
      summaryStubs({
        "GET /orders/o1": { ...order, creator_id: "u9", money_collector_id: null },
      }),
    );

    const app = mountApp(() => []);
    app.session.set({ id: "u8", name: "wer", display_name: "Wer", is_admin: false });
    const rendered = await summaryPage(app, "o1");
    await settle();

    const boxes = [...rendered.querySelectorAll<HTMLInputElement>(".paid-box input")];
    expect(boxes.length).toBe(2);
    expect(boxes.every((box) => box.disabled)).toBe(true);
  });
});
