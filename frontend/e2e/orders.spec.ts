// The order flows, in a real browser.
//
// The sprint's exit criterion is the last test in this file: two people in two
// browsers filling one order and seeing each other's items appear without a
// reload. Everything above it is what has to work for that to mean anything.

import { expect, test } from "@playwright/test";

import { login, loginThroughTheForm, register, seedOrder, seedRestaurant } from "./support";

test.describe("an order", () => {
  test("is created from the overview, with the deadline checked first", async ({
    page,
    request,
  }) => {
    const account = await register(request, "creator");
    await login(request, account);
    const fixture = await seedRestaurant(request);

    await loginThroughTheForm(page, account);
    await page.goto("/");
    await page.getByRole("link", { name: "New order" }).click();

    await page.getByLabel("Restaurant").selectOption({ label: fixture.restaurantName });
    // A deadline after the food arrives: refused here, before anything is sent.
    await page.getByLabel("Food arrives").fill("2030-09-10T12:00");
    await page.getByLabel("Order deadline").fill("2030-09-10T13:00");
    await expect(page.getByText("The deadline has to be before the food arrives.")).toBeVisible();

    await page.getByLabel("Order deadline").fill("2030-09-10T11:00");
    await expect(
      page.getByText("The deadline has to be before the food arrives."),
    ).not.toBeVisible();

    await page.getByRole("button", { name: "New order", exact: true }).click();
    await expect(page).toHaveURL(/\/orders\/[0-9a-f-]{36}$/);
    await expect(page.getByText(fixture.restaurantName).first()).toBeVisible();
  });

  test("takes an item with its options and shows what it costs", async ({ page, request }) => {
    const account = await register(request, "orderer");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    await loginThroughTheForm(page, account);
    await page.goto(`/orders/${orderID}`);

    await page.getByRole("button", { name: "Add an item" }).first().click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Quantity").fill("2");
    await dialog.getByText("Mit Käse").click();

    // 2 × (9.50 + 1.00), computed as the boxes are ticked.
    await expect(dialog.getByText("21.00")).toBeVisible();
    await dialog.getByRole("button", { name: "Save" }).click();

    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText(fixture.itemName).first()).toBeVisible();
    await expect(page.getByText("21.00").first()).toBeVisible();
    // The delivery fee is part of the grand total.
    await expect(page.getByText("24.50").first()).toBeVisible();
  });

  test("shows an anonymous visitor the count and nothing about who ordered", async ({
    page,
    request,
    browser,
  }) => {
    const account = await register(request, "host");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    // One item, added by somebody logged in.
    const filler = await browser.newContext();
    const fillerPage = await filler.newPage();
    await loginThroughTheForm(fillerPage, account);
    await fillerPage.goto(`/orders/${orderID}`);
    await fillerPage.getByRole("button", { name: "Add an item" }).first().click();
    await fillerPage.getByRole("dialog").getByRole("button", { name: "Save" }).click();
    await expect(fillerPage.getByRole("dialog")).toHaveCount(0);
    await filler.close();

    // And now somebody who is not logged in at all.
    await page.goto(`/orders/${orderID}`);
    await expect(page.getByText("1 item so far")).toBeVisible();
    await expect(page.getByText("Who ordered what is visible to logged-in users.")).toBeVisible();
    // The menu is public and still there; the item list is not.
    await expect(page.getByText(fixture.itemName).first()).toBeVisible();
    await expect(page.getByText(account.displayName)).toHaveCount(0);
  });

  test("carries a second browser's item into the first without a reload", async ({
    browser,
    request,
  }) => {
    // The sprint's exit criterion.
    const host = await register(request, "host");
    await login(request, host);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    const guest = await register(request, "guest");

    const first = await browser.newContext();
    const second = await browser.newContext();
    const watching = await first.newPage();
    const ordering = await second.newPage();

    await loginThroughTheForm(watching, host);
    await watching.goto(`/orders/${orderID}`);
    await expect(watching.getByText("Nothing has been ordered yet.")).toBeVisible();

    await loginThroughTheForm(ordering, guest);
    await ordering.goto(`/orders/${orderID}`);
    await ordering.getByRole("button", { name: "Add an item" }).first().click();
    await ordering.getByRole("dialog").getByRole("button", { name: "Save" }).click();
    await expect(ordering.getByRole("dialog")).toHaveCount(0);

    // Nobody reloaded the first window. The event arrived, and the page
    // fetched what it had missed.
    await expect(watching.getByText(guest.displayName)).toBeVisible({ timeout: 15_000 });
    await expect(watching.getByText(fixture.itemName).first()).toBeVisible();

    await first.close();
    await second.close();
  });
});
