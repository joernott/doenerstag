// Clears what previous runs left behind, before this run starts.
//
// Every browser test builds the world it needs through the API and leaves it
// there: a restaurant, a menu, two or three accounts, sometimes an order. That
// is deliberate -- a test that tidied up after itself would delete the evidence
// of why it failed -- but it accumulates. The development database had reached
// 470 restaurants, which makes the application slow to look at and the tables
// tiresome to read.
//
// So the tidying happens at the start of the next run instead of the end of the
// last one. The world the previous run left is still there while somebody is
// looking at it, and gone by the time it matters.
//
// This deletes **everything except the root administrator**: every order, every
// restaurant, every other account. On a machine that is only ever a test target
// that is what is wanted. It is not what anybody wants on a machine holding
// real orders, which is why it runs from the test suite and nowhere else.

import { request, type APIRequestContext } from "@playwright/test";

const baseURL = process.env["DOENER_E2E_URL"] ?? "https://localhost:8443";

// The account the installer creates. CI provisions it with this password; a
// developer whose machine differs sets DOENER_E2E_ROOT_PASSWORD.
const rootPassword =
  process.env["DOENER_E2E_ROOT_PASSWORD"] ?? "Korrektes Pferd Batterie Klammer1";

interface Identified {
  id: string;
  name?: string;
}

async function csrf(api: APIRequestContext): Promise<string> {
  const state = await api.storageState();
  const cookie = state.cookies.find((c) => c.name === "doener_csrf");
  return cookie?.value ?? "";
}

/** Deletes every item of a collection, and says how many it removed. */
async function clear(
  api: APIRequestContext,
  path: string,
  plural: string,
  keep: (item: Identified) => boolean = () => false,
): Promise<number> {
  const response = await api.get(`/api/v1${path}`);
  if (!response.ok()) {
    throw new Error(`listing ${plural}: ${response.status()} ${await response.text()}`);
  }
  const body = (await response.json()) as Record<string, Identified[]>;
  const items = body[plural] ?? [];

  let removed = 0;
  for (const item of items) {
    if (keep(item)) {
      continue;
    }
    const deleted = await api.delete(`/api/v1${path}/${item.id}`, {
      headers: { "X-CSRF-Token": await csrf(api) },
    });
    // A refusal is reported rather than thrown: one restaurant that will not
    // go is not a reason to abandon the whole run, and the count says how much
    // was actually cleared.
    if (deleted.ok()) {
      removed++;
    } else {
      console.warn(
        `  could not delete ${plural} ${item.id}: ${deleted.status()} ${await deleted.text()}`,
      );
    }
  }
  return removed;
}

export default async function globalSetup(): Promise<void> {
  const api = await request.newContext({ baseURL, ignoreHTTPSErrors: true });

  try {
    const login = await api.post("/api/v1/auth/login", {
      data: { name: "root", password: rootPassword },
    });
    if (!login.ok()) {
      // Not fatal. A developer running against a server whose root password
      // differs should get their tests, not a refusal to start; the database
      // simply stays as crowded as it was.
      console.warn(
        `e2e cleanup: could not log in as root (${login.status()}). ` +
          `Set DOENER_E2E_ROOT_PASSWORD to enable it. Skipping.`,
      );
      return;
    }

    // Orders first: a restaurant cannot be deleted while one points at it.
    const orders = await clear(api, "/orders", "orders");
    const restaurants = await clear(api, "/restaurants", "restaurants");
    // Everything but the administrator running this.
    const users = await clear(api, "/users", "users", (user) => user.name === "root");

    console.log(
      `e2e cleanup: removed ${orders} order(s), ${restaurants} restaurant(s), ${users} account(s)`,
    );
  } catch (error) {
    console.warn(`e2e cleanup: skipped (${String(error)})`);
  } finally {
    await api.dispose();
  }
}
