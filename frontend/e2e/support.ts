// What the browser tests need before they can start: an account, a restaurant
// and a menu.
//
// Built through the API rather than by inserting rows, so the fixture goes
// through the same validation everything else does (docs/12_testing.md). Each
// run makes its own world with a unique suffix, because the tests run against a
// database that other runs have already used.

import type { APIRequestContext, Page } from "@playwright/test";

export interface Account {
  name: string;
  displayName: string;
  password: string;
  id: string;
}

/** A password that meets three of the five classes without contortion. */
export const PASSWORD = "Korrektes Pferd Batterie Klammerß";

/** Something no other run will have used. */
export function unique(prefix: string): string {
  return `${prefix}_${Date.now().toString(36)}${Math.floor(Math.random() * 1000)}`;
}

/** Registers an account and returns it. The request context keeps its cookies. */
export async function register(request: APIRequestContext, prefix = "user"): Promise<Account> {
  const name = unique(prefix);
  // The token is sent even for the first registration, when there is none:
  // once this context has registered anybody, it holds a session, and the
  // server rightly refuses a cookie-authenticated write without the header.
  const response = await request.post("/api/v1/auth/register", {
    data: { name, display_name: name, email: "", password: PASSWORD },
    headers: { "X-CSRF-Token": await csrf(request) },
  });
  if (!response.ok()) {
    throw new Error(`registering ${name} failed: ${response.status()} ${await response.text()}`);
  }
  const body = (await response.json()) as { user: { id: string } };
  return { name, displayName: name, password: PASSWORD, id: body.user.id };
}

/**
 * Logs in through the API.
 *
 * Registration already leaves the context logged in, so this is for the tests
 * that want a second session or an explicit one. It sends the CSRF token,
 * because by then the context has a session cookie and the server refuses a
 * cookie-authenticated write without the header -- which is the rule working,
 * not a nuisance to be worked around.
 */
export async function login(request: APIRequestContext, account: Account): Promise<void> {
  const response = await request.post("/api/v1/auth/login", {
    data: { name: account.name, password: account.password },
    headers: { "X-CSRF-Token": await csrf(request) },
  });
  if (!response.ok()) {
    throw new Error(`logging ${account.name} in failed: ${response.status()}`);
  }
}

/** The CSRF token the server set, which every write has to echo back. */
export async function csrf(request: APIRequestContext): Promise<string> {
  const state = await request.storageState();
  const cookie = state.cookies.find((entry) => entry.name === "doener_csrf");
  return cookie?.value ?? "";
}

async function write<T>(
  request: APIRequestContext,
  method: "post" | "patch",
  path: string,
  data: unknown,
): Promise<T> {
  const response = await request[method](path, {
    data,
    headers: { "X-CSRF-Token": await csrf(request) },
  });
  if (!response.ok()) {
    throw new Error(`${method.toUpperCase()} ${path}: ${response.status()} ${await response.text()}`);
  }
  return (await response.json()) as T;
}

export interface Fixture {
  restaurantID: string;
  restaurantName: string;
  categoryID: string;
  itemID: string;
  itemName: string;
  modificationID: string;
}

/**
 * A restaurant with one category, one dish and one option.
 *
 * Small on purpose: every test that needs more builds it, and a fixture nobody
 * reads in full is a fixture nobody trusts.
 */
export async function seedRestaurant(request: APIRequestContext): Promise<Fixture> {
  const contactTypes = (await (await request.get("/api/v1/contact-types")).json()) as {
    contact_types: { id: string; code: string }[];
  };
  const phone = contactTypes.contact_types.find((type) => type.code === "phone");

  const restaurantName = unique("Pinar");
  const restaurant = await write<{ id: string }>(request, "post", "/api/v1/restaurants", {
    name: restaurantName,
    currency_code: "CHF",
    min_order_value_cents: 2000,
    delivery_fee_cents: 350,
    contacts: [{ contact_type_id: phone?.id, value: "+41 44 123 45 67", label: "", sort_order: 10 }],
  });

  const category = await write<{ id: string }>(
    request,
    "post",
    `/api/v1/restaurants/${restaurant.id}/categories`,
    { name: "Kebap", sort_order: 10 },
  );

  const itemName = unique("Döner");
  const item = await write<{ id: string }>(
    request,
    "post",
    `/api/v1/restaurants/${restaurant.id}/menu-items`,
    {
      category_id: category.id,
      external_id: "12",
      name: itemName,
      description: "Mit allem",
      price_cents: 950,
      available: true,
    },
  );

  const modification = await write<{ id: string }>(
    request,
    "post",
    `/api/v1/restaurants/${restaurant.id}/menu-items/${item.id}/modifications`,
    { name: "Mit Käse", price_delta_cents: 100, sort_order: 10 },
  );

  return {
    restaurantID: restaurant.id,
    restaurantName,
    categoryID: category.id,
    itemID: item.id,
    itemName,
    modificationID: modification.id,
  };
}

/** An order at that restaurant, open for the next few hours. */
export async function seedOrder(
  request: APIRequestContext,
  restaurantID: string,
): Promise<string> {
  const hours = (count: number): string =>
    new Date(Date.now() + count * 3600 * 1000).toISOString();

  const order = await write<{ id: string }>(request, "post", "/api/v1/orders", {
    restaurant_id: restaurantID,
    fulfilment: "pickup",
    fulfilment_at: hours(3),
    deadline_at: hours(2),
    money_collector: "",
    pickup_person: "",
  });
  return order.id;
}

/**
 * Logs a browser page in through the form, which is the part worth exercising.
 *
 * The session cookie is HttpOnly and SameSite=Strict, so it cannot be planted
 * from script: the only way a page becomes logged in is by logging in.
 */
export async function loginThroughTheForm(page: Page, account: Account): Promise<void> {
  await page.goto("/account");

  // Both panels are in the document -- the register one is hidden, not absent
  // -- so the fields are looked for inside the login panel rather than on the
  // page, where "User name" would match twice.
  const panel = page.locator("[role=tabpanel]").first();
  await panel.getByLabel("User name").fill(account.name);
  await panel.getByLabel("Password", { exact: true }).fill(account.password);
  await panel.getByRole("button", { name: "Log in" }).click();

  await page.getByRole("link", { name: account.displayName }).waitFor();
}
