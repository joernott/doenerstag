// The chrome, in a real browser: the things a headless DOM cannot decide.
//
// A cookie surviving a reload is a browser behaviour, not an application one,
// and the only way to know the application uses it correctly is to reload a
// real browser.

import { expect, test } from "@playwright/test";

import { login, loginThroughTheForm, register, seedRestaurant } from "./support";

test.describe("the shell", () => {
  test("remembers the language and the theme across a reload", async ({ page }) => {
    await page.goto("/");

    await page.getByLabel("Language").selectOption("de");

    // The visible chrome is in German, and so is the document itself. The menu
    // entries are deliberately not asserted here: they live in a closed
    // dropdown, and "Bestellungen" is also part of the logo's own label.
    await expect(page.getByRole("link", { name: "Anmelden / Registrieren" })).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("lang", "de");

    await page.getByRole("button", { name: /dunkel|hell|dark|light/i }).click();
    const theme = await page.locator("html").getAttribute("data-theme");

    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("lang", "de");
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme ?? "light");
  });

  test("registers, logs out and logs in again", async ({ page, request }) => {
    const account = await register(request, "shell");

    await loginThroughTheForm(page, account);
    await expect(page.getByRole("link", { name: account.displayName })).toBeVisible();

    await page.getByRole("button", { name: "Log out" }).click();
    await expect(page.getByRole("link", { name: "Log in / Register" })).toBeVisible();

    await loginThroughTheForm(page, account);
    await expect(page.getByRole("link", { name: account.displayName })).toBeVisible();
  });

  test("walks the modal by keyboard and closes it with Escape", async ({ page, request }) => {
    const account = await register(request, "keyboard");
    await loginThroughTheForm(page, account);

    await page.goto("/account");
    await page.getByRole("button", { name: "Delete my account" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    // Cancel has the focus, so a stray Enter destroys nothing.
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeFocused();

    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    // ...and the account is still there.
    await expect(page.getByRole("link", { name: account.displayName })).toBeVisible();
  });

  test("a restaurant and its menu can be entered in the browser", async ({ page, request }) => {
    const account = await register(request, "builder");
    await loginThroughTheForm(page, account);

    await page.goto("/restaurants");
    await page.getByRole("link", { name: "New restaurant" }).click();

    const name = `Neu ${Date.now().toString(36)}`;
    await page.getByLabel("Name").fill(name);
    await page.getByLabel("Currency").selectOption("CHF");
    await page.getByLabel("Value").fill("+41 44 000 00 00");
    await page.getByRole("button", { name: "Create" }).click();

    await expect(page).toHaveURL(/\/restaurants\/[0-9a-f-]{36}$/);

    await page.getByLabel("Category").first().fill("Kebap");
    await page.getByRole("button", { name: "Add a category" }).click();
    await expect(page.getByRole("button", { name: "Move up" })).toBeVisible();

    await page.getByRole("button", { name: "Add an item" }).first().click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name", { exact: true }).fill("Falafel");
    await dialog.getByLabel("Price").fill("7,50");
    await dialog.getByText("Vegan").click();
    await dialog.getByRole("button", { name: "Save" }).click();

    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText("Falafel")).toBeVisible();
    // Typed with a comma, priced in CHF, shown in the interface's own format.
    await expect(page.getByText("CHF 7.50")).toBeVisible();
    await expect(page.getByText("Vegan").first()).toBeVisible();
  });
});

test.describe("what an anonymous visitor is offered", () => {
  test("sees the restaurants and is sent to the login page to add one", async ({
    page,
    request,
  }) => {
    // The API context and the browser have separate cookie jars, so seeding
    // through the first leaves the second anonymous.
    const account = await register(request, "public");
    await login(request, account);
    const fixture = await seedRestaurant(request);

    await page.goto("/restaurants");
    await expect(page.getByText(fixture.restaurantName)).toBeVisible();
    // The plus tile leads to the login page rather than to a form that would
    // refuse to save.
    await expect(page.getByRole("link", { name: "New restaurant" })).toHaveAttribute(
      "href",
      "/account",
    );
  });
});
