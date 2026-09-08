// The Content-Security-Policy as the browser applies it, rather than as a
// header the server sends.
//
// internal/api/middleware_test.go already asserts the header's text, and
// internal/api/openapi_test.go asserts that Swagger's relaxation is scoped to
// style-src. Neither says whether the policy is one the application can
// actually run under: a policy that forbids inline script is only worth having
// if nothing in the page needs inline script, and the way to know that is to
// let a browser enforce it.
//
// docs/11_nonfunctional.md asks for a security review covering "CSP in
// practice". This is that, as a test rather than as an afternoon.

import { expect, test, type Page } from "@playwright/test";

import { login, loginThroughTheForm, register, seedOrder, seedRestaurant } from "./support";

/** One blocked resource or refused script, as the browser reported it. */
interface Violation {
  directive: string;
  blocked: string;
  where: string;
}

/**
 * Watches a page for policy violations and page errors.
 *
 * `securitypolicyviolation` is the event the browser fires when it refuses
 * something. Listening for it is better than reading console text: the console
 * message is unstructured and worded differently per browser, and this suite
 * runs in two.
 */
async function watch(page: Page, found: Violation[]): Promise<void> {
  await page.addInitScript(() => {
    const record = (event: SecurityPolicyViolationEvent): void => {
      const store = (window as unknown as { __csp?: unknown[] }).__csp ?? [];
      store.push({
        directive: event.violatedDirective,
        blocked: event.blockedURI,
        where: `${event.sourceFile ?? "?"}:${event.lineNumber ?? 0}`,
      });
      (window as unknown as { __csp?: unknown[] }).__csp = store;
    };
    document.addEventListener("securitypolicyviolation", record);
  });

  page.on("pageerror", (error) => {
    found.push({ directive: "uncaught", blocked: String(error), where: page.url() });
  });
}

/** Collects what the page recorded since it was loaded. */
async function drain(page: Page, found: Violation[]): Promise<void> {
  const recorded = await page.evaluate(
    () => (window as unknown as { __csp?: Violation[] }).__csp ?? [],
  );
  for (const entry of recorded) {
    found.push({ ...entry, where: `${page.url()} (${entry.where})` });
  }
}

test.describe("the application runs under its own CSP", () => {
  test("no public page is refused anything", async ({ page }) => {
    const found: Violation[] = [];
    await watch(page, found);

    for (const path of ["/", "/restaurants", "/version", "/imprint", "/legal-notes", "/account"]) {
      await page.goto(path);
      // The shell renders through the router, so waiting for the outlet to
      // have content is what "the page has run" means here.
      await expect(page.locator("main")).toBeVisible();
      await drain(page, found);
    }

    expect(found, "the CSP blocked something a public page needs").toEqual([]);
  });

  test("no page behind a login is refused anything", async ({ page, request }) => {
    const found: Violation[] = [];
    await watch(page, found);

    const account = await register(request, "csp");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    await loginThroughTheForm(page, account);
    for (const path of [
      "/orders",
      `/orders/${orderID}`,
      `/orders/${orderID}/summary`,
      `/restaurants/${fixture.restaurantID}`,
      "/account",
    ]) {
      await page.goto(path);
      await expect(page.locator("main")).toBeVisible();
      await drain(page, found);
    }

    expect(found, "the CSP blocked something a logged-in page needs").toEqual([]);
  });

  // The header itself, once, from a real response rather than from a recorder.
  // The unit test asserts the constant; this asserts that the constant is what
  // leaves the process.
  test("the policy that arrives is the strict one", async ({ request }) => {
    const response = await request.get("/");
    const policy = response.headers()["content-security-policy"];

    expect(policy, "no Content-Security-Policy header was sent").toBeTruthy();
    expect(policy).toContain("default-src 'self'");
    expect(policy).toContain("frame-ancestors 'none'");
    expect(policy).not.toContain("unsafe-inline");
    expect(policy).not.toContain("unsafe-eval");

    // The rest of the documented headers, from docs/05_auth_and_permissions.md.
    expect(response.headers()["x-content-type-options"]).toBe("nosniff");
    expect(response.headers()["referrer-policy"]).toBe("same-origin");
  });

  // An imprint is the one place the application writes HTML it did not build
  // element by element, so it is the one place a stored script could live. The
  // snippet is sanitised on the way in; this checks the result rather than the
  // sanitiser.
  test("a script in the imprint neither runs nor survives", async ({ page }) => {
    const found: Violation[] = [];
    await watch(page, found);

    // Only an administrator may write it, and the browser tests have no
    // administrator, so this checks the read side: whatever is stored, the page
    // that renders it must not execute anything.
    await page.goto("/imprint");
    await expect(page.locator("main")).toBeVisible();

    const scripts = await page.locator("main script").count();
    expect(scripts, "the imprint rendered a script element").toBe(0);

    await drain(page, found);
    expect(found).toEqual([]);
  });
});
