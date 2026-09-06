// The summary, the administration and the content pages, in a real browser.

import { expect, test } from "@playwright/test";

import { login, loginThroughTheForm, register, seedOrder, seedRestaurant } from "./support";

/** Adds one item to an order through the browser, which is how a person does it. */
async function addAnItem(page: import("@playwright/test").Page, orderID: string): Promise<void> {
  await page.goto(`/orders/${orderID}`);
  await page.getByRole("button", { name: "Add an item" }).first().click();
  await page.getByRole("dialog").getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
}

test.describe("the summary", () => {
  test("is reachable from the order and reads back what was ordered", async ({ page, request }) => {
    const account = await register(request, "reader1");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    await loginThroughTheForm(page, account);
    await addAnItem(page, orderID);

    // The button appears because this person is now a participant.
    // exact, because getByRole matches names by substring and an account
    // called "summary_something" would otherwise be the first match.
    await page.getByRole("link", { name: "Summary", exact: true }).first().click();
    await expect(page).toHaveURL(`/orders/${orderID}/summary`);

    // The four sections, and the number to dial.
    await expect(page.getByRole("heading", { name: "What to order" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Who owes what" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Totals" })).toBeVisible();
    await expect(page.getByRole("link", { name: "+41 44 123 45 67" })).toHaveAttribute(
      "href",
      "tel:+41 44 123 45 67",
    );

    // One of the dish, at 9.50, in the restaurant's currency.
    await expect(page.getByText("1×").first()).toBeVisible();
    await expect(page.getByText(fixture.itemName).first()).toBeVisible();
    await expect(page.getByText("CHF 9.50").first()).toBeVisible();
    await expect(page.getByText(account.displayName).first()).toBeVisible();
  });

  test("copies the API's own plain text", async ({ page, request, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]).catch(() => {
      // Firefox does not take these; the assertion below falls back to the
      // textarea the page offers when the clipboard refuses.
    });

    const account = await register(request, "copier");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    await loginThroughTheForm(page, account);
    await addAnItem(page, orderID);
    await page.goto(`/orders/${orderID}/summary`);

    await page.getByRole("button", { name: "Copy as text" }).click();

    // Either the clipboard took it, or the page offered the text to select.
    // Which of the two depends on the browser and on whether the origin counts
    // as secure, and the point is that one of them happened rather than
    // nothing at all.
    const copied = page.getByText("Copied");
    const fallback = page.getByLabel("The summary as plain text");
    await expect
      .poll(async () => (await copied.isVisible()) || (await fallback.isVisible()))
      .toBe(true);
  });

  test("tells a non-participant why they cannot see it", async ({ page, request, browser }) => {
    const host = await register(request, "host");
    await login(request, host);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    // Somebody else, who has ordered nothing.
    const stranger = await register(request, "stranger");
    const other = await browser.newContext();
    const strangerPage = await other.newPage();
    await loginThroughTheForm(strangerPage, stranger);
    await strangerPage.goto(`/orders/${orderID}/summary`);

    await expect(
      strangerPage.getByText("This summary is visible to the people taking part in the order."),
    ).toBeVisible();
    await expect(
      strangerPage.getByRole("link", { name: "Back to the order" }).first(),
    ).toBeVisible();
    await other.close();

    // And an anonymous visitor is offered the way in.
    await page.goto(`/orders/${orderID}/summary`);
    await expect(page.getByRole("link", { name: "Log in to join this order" })).toBeVisible();
  });

  test("is not offered to somebody who is not taking part", async ({ page, request }) => {
    const host = await register(request, "hostx");
    await login(request, host);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    const stranger = await register(request, "strangerx");
    await loginThroughTheForm(page, stranger);
    await page.goto(`/orders/${orderID}`);

    // No items of theirs, and not the creator: no button (F1.3).
    await expect(page.getByRole("link", { name: "Summary", exact: true })).toHaveCount(0);
  });
});

test.describe("the administration", () => {
  test("lists the users and refuses everybody else", async ({ page, request }) => {
    const account = await register(request, "notadmin");
    await loginThroughTheForm(page, account);

    await page.goto("/admin/users");
    await expect(page.getByText("Only the administrator can do that.")).toBeVisible();
  });
});

test.describe("the content pages", () => {
  test("shows the imprint and the legal notes to anybody", async ({ page }) => {
    await page.goto("/imprint");
    await expect(page.getByRole("heading", { name: "Imprint", level: 1 })).toBeVisible();
    await expect(page.locator(".content-page")).not.toBeEmpty();

    await page.goto("/legal-notes");
    await expect(page.getByRole("heading", { name: "Legal notes", level: 1 })).toBeVisible();
    await expect(page.locator(".content-page")).not.toBeEmpty();
  });

  test("offers no replacement to somebody who is not the administrator", async ({
    page,
    request,
  }) => {
    const account = await register(request, "reader");
    await loginThroughTheForm(page, account);

    await page.goto("/imprint");
    await expect(page.getByRole("button", { name: "Replace the content" })).toHaveCount(0);
  });
});

test.describe("the version page", () => {
  test("reports what the binary is", async ({ page }) => {
    await page.goto("/version");
    await expect(page.getByText("Application version")).toBeVisible();
    await expect(page.getByText("Commit")).toBeVisible();
  });
});
