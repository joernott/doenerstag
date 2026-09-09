// Clears what previous runs of *this suite* left behind, before this run starts.
//
// Every browser test builds the world it needs through the API and leaves it
// there: a restaurant, a menu, two or three accounts, sometimes an order. That
// is deliberate -- a test that tidied up after itself would delete the evidence
// of why it failed -- but it accumulates. The development database had reached
// 500 restaurants, 78 orders and 792 accounts, which makes the application slow
// to look at and the tables tiresome to read.
//
// So the tidying happens at the start of the next run instead of the end of the
// last one. The world the previous run left is still there while somebody is
// looking at it, and gone by the time it matters.
//
// **It deletes only what this suite created.** Everything the fixtures invent is
// named through unique(), which prefixes E2E_PREFIX, and nothing without that
// prefix is touched. The first version of this matched test data by the shape of
// its name instead -- a word, an underscore, a timestamp -- which was close
// enough to be convincing and close enough to delete an account a person had
// made. A prefix nobody types by accident is not a guess; a pattern that
// describes most test names is.

import { request, type APIRequestContext } from "@playwright/test";

import { E2E_PREFIX } from "./support";

const baseURL = process.env["DOENER_E2E_URL"] ?? "https://localhost:8443";

// The administrator to act as. The installer calls it root, and CI provisions
// it with this password; a developer whose machine differs sets these.
const adminUser = process.env["DOENER_E2E_ROOT_USER"] ?? "root";
const adminPassword =
  process.env["DOENER_E2E_ROOT_PASSWORD"] ?? "Korrektes Pferd Batterie Klammer1";

/** Whether a name was invented by this suite. */
function ours(name: string | undefined): boolean {
  return typeof name === "string" && name.startsWith(E2E_PREFIX);
}

async function csrf(api: APIRequestContext): Promise<string> {
  const state = await api.storageState();
  return state.cookies.find((c) => c.name === "doener_csrf")?.value ?? "";
}

interface Listed {
  id: string;
  name?: string;
  restaurant_name?: string;
}

/**
 * Deletes the items of a collection that this suite created.
 *
 * `identify` says which field carries the name, because an order has no name of
 * its own: it is recognised by the restaurant it was placed at, which this
 * suite also created and also marked.
 */
async function clear(
  api: APIRequestContext,
  path: string,
  plural: string,
  identify: (item: Listed) => string | undefined,
): Promise<{ removed: number; kept: number }> {
  const response = await api.get(`/api/v1${path}`);
  if (!response.ok()) {
    throw new Error(`listing ${plural}: ${response.status()} ${await response.text()}`);
  }
  const body = (await response.json()) as Record<string, Listed[]>;
  const items = body[plural] ?? [];

  let removed = 0;
  let kept = 0;
  for (const item of items) {
    if (!ours(identify(item))) {
      kept++;
      continue;
    }
    const deleted = await api.delete(`/api/v1${path}/${item.id}`, {
      headers: { "X-CSRF-Token": await csrf(api) },
    });
    // A refusal is reported rather than thrown: one restaurant that will not go
    // is not a reason to abandon the run, and the count says what happened.
    if (deleted.ok()) {
      removed++;
    } else {
      console.warn(
        `  could not delete ${plural} ${item.id}: ${deleted.status()} ${await deleted.text()}`,
      );
    }
  }
  return { removed, kept };
}

export default async function globalSetup(): Promise<void> {
  const api = await request.newContext({ baseURL, ignoreHTTPSErrors: true });

  try {
    const login = await api.post("/api/v1/auth/login", {
      data: { name: adminUser, password: adminPassword },
    });
    if (!login.ok()) {
      // Not fatal. A developer running against a server whose root password
      // differs should get their tests, not a refusal to start; the database
      // simply stays as crowded as it was.
      console.warn(
        `e2e cleanup: could not log in as ${adminUser} (${login.status()}). ` +
          `Set DOENER_E2E_ROOT_PASSWORD to enable it. Skipping.`,
      );
      return;
    }

    // Orders first: a restaurant cannot be deleted while one points at it.
    const orders = await clear(api, "/orders", "orders", (o) => o.restaurant_name);
    const restaurants = await clear(api, "/restaurants", "restaurants", (r) => r.name);
    const users = await clear(api, "/users", "users", (u) => u.name);

    console.log(
      `e2e cleanup: removed ${orders.removed} order(s), ${restaurants.removed} restaurant(s), ` +
        `${users.removed} account(s) created by this suite; ` +
        `left ${orders.kept + restaurants.kept + users.kept} other record(s) alone`,
    );
  } catch (error) {
    console.warn(`e2e cleanup: skipped (${String(error)})`);
  } finally {
    await api.dispose();
  }
}
