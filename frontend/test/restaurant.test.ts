// The restaurant page and the menu editor.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { restaurantPage } from "../src/pages/restaurant";
import { restaurantsPage } from "../src/pages/restaurants";
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
  opening_hours: [
    { id: "h1", day_of_week: 1, start: "11:00", end: "14:00", crosses_midnight: false },
    { id: "h2", day_of_week: 1, start: "17:00", end: "02:00", crosses_midnight: true },
  ],
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
};

function restaurantStubs(): Record<string, unknown> {
  return {
    ...referenceStubs,
    "GET /restaurants/r1": restaurant,
    "GET /restaurants/r1/categories": { categories: [{ id: "cat1", name: "Kebap", sort_order: 10 }] },
    "GET /restaurants/r1/menu-items": { menu_items: [menuItem] },
  };
}

beforeEach(() => {
  document.body.replaceChildren();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the overview", () => {
  it("puts the plus tile first and lists the restaurants alphabetically", async () => {
    stubServer({
      "GET /restaurants": {
        restaurants: [
          { ...restaurant, id: "r2", name: "Zorbas" },
          { ...restaurant, id: "r1", name: "Ali Baba" },
        ],
      },
      "GET /restaurants/r1": restaurant,
      "GET /restaurants/r2": restaurant,
      "GET /restaurants/r1/menu-items": { menu_items: [menuItem] },
      "GET /restaurants/r2/menu-items": { menu_items: [] },
    });

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await restaurantsPage(app);

    const links = [...rendered.querySelectorAll("a")].map((link) => link.getAttribute("href"));
    expect(links).toEqual(["/restaurants/new", "/restaurants/r1", "/restaurants/r2"]);
  });

  it("sends an anonymous visitor to the login page instead of the create form", async () => {
    stubServer({ "GET /restaurants": { restaurants: [] } });

    const app = mountApp(() => []);
    const rendered = await restaurantsPage(app);

    expect(rendered.querySelector("a")?.getAttribute("href")).toBe("/account");
  });

  it("shows the first contact, the item count and the opening state on a tile", async () => {
    stubServer({
      "GET /restaurants": { restaurants: [restaurant] },
      "GET /restaurants/r1": restaurant,
      "GET /restaurants/r1/menu-items": { menu_items: [menuItem, { ...menuItem, id: "m2" }] },
    });

    const app = mountApp(() => []);
    const rendered = await restaurantsPage(app);

    const tile = [...rendered.querySelectorAll(".tile")][1];
    expect(tile?.textContent).toContain("+41 44 123 45 67");
    expect(tile?.textContent).toContain(app.t.t("restaurant.items", { count: 2 }));
    // Open or closed depends on the clock; either way it is stated in words.
    const state = tile?.textContent ?? "";
    expect(
      state.includes(app.t.t("restaurant.open_now")) || state.includes(app.t.t("restaurant.closed")),
    ).toBe(true);
  });

  it("still renders a tile when a restaurant's detail cannot be read", async () => {
    // One restaurant failing must not empty the whole overview.
    stubServer({ "GET /restaurants": { restaurants: [restaurant] } });

    const app = mountApp(() => []);
    const rendered = await restaurantsPage(app);

    const tile = [...rendered.querySelectorAll(".tile")][1];
    expect(tile?.textContent).toContain(restaurant.name);
    expect(tile?.textContent).toContain(app.t.t("restaurant.no_contact"));
  });
});

describe("the restaurant page", () => {
  it("presents the four sections as tabs, with the menu first", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    const tabs = [...rendered.querySelectorAll("[role='tab']")].map((entry) => entry.textContent);
    expect(tabs).toEqual([
      app.t.t("restaurant.menu"),
      app.t.t("restaurant.data"),
      app.t.t("restaurant.contacts"),
      app.t.t("restaurant.opening_hours"),
    ]);

    // The menu is the one somebody almost always came for, so it is the one
    // that is open; the other three are in the document and hidden, which is
    // what makes them a click away rather than a fetch away.
    const panels = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")];
    expect(panels.length).toBe(4);
    expect(panels.map((panel) => panel.hidden)).toEqual([false, true, true, true]);
    // The panel is named by its tab, so it carries no heading repeating that
    // name. What identifies it is what it holds.
    expect(panels[0]?.textContent).toContain(app.t.t("item.add"));
    expect(panels[0]?.textContent).toContain(app.t.t("menu.category.add"));
  });

  it("shows the money in the restaurant's own currency", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    // The restaurant is priced in CHF and the interface is in English.
    const price = rendered.querySelector(".menu-price")?.textContent ?? "";
    expect(price).toContain("9.50");
    expect(price).toContain("CHF");

    // The minimum order value is an editable amount, so it is a plain number.
    const minimum = rendered.querySelector<HTMLInputElement>("input[name='min_order_value']");
    expect(minimum?.value).toBe("20.00");
  });

  it("will not let the last contact be deleted", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    const remove = [...rendered.querySelectorAll("button")].find(
      (control) => control.textContent === app.t.t("action.remove"),
    );
    expect(remove?.disabled).toBe(true);
    expect(remove?.title).toBe(app.t.t("restaurant.contact.last"));
  });

  it("calls a closing time before its opening time a crossed midnight, not an error", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    const hints = [...rendered.querySelectorAll(".hint-inline")].map((hint) => hint.textContent);
    expect(hints).toEqual(["", app.t.t("restaurant.crosses_midnight")]);
  });

  it("renders the menu grouped by category, with its markers", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    expect(rendered.querySelector(".menu-item strong")?.textContent).toBe("Döner Kebap");
    expect(rendered.querySelector(".menu-number")?.textContent).toBe("12");

    // Tags and allergens are translated from their codes and carry text, never
    // colour alone.
    const chips = [...rendered.querySelectorAll(".chip")].map((chip) => chip.textContent);
    expect(chips).toEqual([app.t.t("tag.vegan"), app.t.t("allergen.gluten")]);
  });

  it("says an unavailable item is sold out in words", async () => {
    const stubs = restaurantStubs();
    stubs["GET /restaurants/r1/menu-items"] = { menu_items: [{ ...menuItem, available: false }] };
    stubServer(stubs);

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    expect(rendered.querySelector(".badge")?.textContent).toBe(app.t.t("menu.item.unavailable"));
  });

  it("opens the item editor with the item's classification ticked", async () => {
    stubServer({
      ...restaurantStubs(),
      "GET /restaurants/r1/menu-items/m1": { ...menuItem, modifications: [] },
    });

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "r1");
    await settle();

    // Scoped to the item row: every category has an Edit button of its own
    // now, and an unscoped search for the word finds the first category.
    const edit = rendered.querySelector<HTMLButtonElement>(".menu-item button");
    expect(edit?.textContent).toBe(app.t.t("action.edit"));
    edit?.click();
    await settle();

    const modal = document.querySelector(".modal");
    expect(modal).not.toBeNull();

    const checked = [...(modal?.querySelectorAll<HTMLInputElement>("input:checked") ?? [])];
    const labels = checked.map((box) => box.parentElement?.textContent);
    // Vegan, gluten and "orderable"; the additive is not ticked.
    expect(labels).toContain(app.t.t("tag.vegan"));
    expect(labels).toContain(app.t.t("allergen.gluten"));
    expect(labels).not.toContain(app.t.t("additive.colouring"));
  });

  it("offers only what a non-administrator may do", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await restaurantPage(app, "r1");
    await settle();

    // Deleting the restaurant is offered but not available, and says which of
    // the two reasons it is unavailable for. Showing it disabled rather than
    // hiding it is deliberate: "you cannot do this, and here is why" is more
    // use than a button that is silently absent.
    const remove = [...rendered.querySelectorAll("button")].find(
      (control) => control.textContent === app.t.t("restaurant.delete"),
    );
    expect(remove?.disabled).toBe(true);
    expect(remove?.title).toBe(app.t.t("restaurant.delete.admin_only"));

    const labels = [...rendered.querySelectorAll("button")].map((control) => control.textContent);
    expect(labels).not.toContain(app.t.t("menu.category.delete"));
    // But the menu is still editable: everything except deletion is any
    // logged-in user's to do.
    expect(labels).toContain(app.t.t("item.add"));
    expect(labels).toContain(app.t.t("menu.category.add"));
  });

  it("keeps Save quiet until something has changed", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "jo", display_name: "Jo", is_admin: false });
    const rendered = await restaurantPage(app, "r1");
    await settle();

    const save = [...rendered.querySelectorAll("button")].find(
      (control) => control.textContent === app.t.t("action.save"),
    );
    expect(save?.disabled).toBe(true);

    const name = rendered.querySelector<HTMLInputElement>("input[name='name']");
    if (!name || !save) {
      throw new Error("the restaurant form is missing its name field or its save button");
    }

    name.value = `${name.value} am Markt`;
    name.dispatchEvent(new Event("input", { bubbles: true }));
    expect(save.disabled).toBe(false);

    // And back again: dirty means "differs from what was loaded", not
    // "somebody pressed a key".
    name.value = restaurant.name;
    name.dispatchEvent(new Event("input", { bubbles: true }));
    expect(save.disabled).toBe(true);
  });

  it("offers the administrator the deletions as well", async () => {
    stubServer(restaurantStubs());

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "root", display_name: "Root", is_admin: true });
    const rendered = await restaurantPage(app, "r1");
    await settle();

    const labels = [...rendered.querySelectorAll("button")].map((control) => control.textContent);
    expect(labels).toContain(app.t.t("restaurant.delete"));
    expect(labels).toContain(app.t.t("menu.category.delete"));
  });

  it("creates a restaurant with the one contact the server insists on", async () => {
    const { calls } = stubServer({
      ...referenceStubs,
      "POST /restaurants": { ...restaurant, id: "r9" },
    });

    const app = mountApp(() => []);
    const rendered = await restaurantPage(app, "new");
    await settle();

    const name = rendered.querySelector<HTMLInputElement>("input[name='name']");
    const contact = rendered.querySelector<HTMLInputElement>("input[name='contact_value']");
    if (!name || !contact) {
      throw new Error("the create form is missing its fields");
    }
    name.value = "Neu";
    contact.value = "+41 44 000 00 00";

    rendered.querySelector("form")?.dispatchEvent(new Event("submit"));
    await settle();

    const created = calls.find((call) => call.method === "POST");
    expect(created?.body).toMatchObject({
      name: "Neu",
      currency_code: "EUR",
      contacts: [{ contact_type_id: "ct-phone", value: "+41 44 000 00 00" }],
    });
  });
});
