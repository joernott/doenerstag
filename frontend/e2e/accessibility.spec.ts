// The accessibility pass: axe-core on every page, in both themes.
//
// docs/12_testing.md asks for exactly this, and docs/06_ui_ux.md sets the
// target at WCAG 2.1 AA. axe cannot decide whether a page makes sense -- no
// tool can -- but it catches the mechanical failures reliably: contrast,
// missing names, orphaned labels, landmarks, headings out of order.
//
// Serious and critical violations fail the build. Minor and moderate ones are
// reported and do not, because the ones axe rates that way here are judgement
// calls it cannot make (an empty region on a page whose content has not loaded
// yet, for instance) and a suite that cries wolf is a suite people stop
// reading.

import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

import { login, loginThroughTheForm, register, seedOrder, seedRestaurant } from "./support";

const THEMES = ["dark", "light"] as const;

/** Sets the theme the way the toggle does, before the page is scanned. */
async function useTheme(page: Page, theme: (typeof THEMES)[number]): Promise<void> {
  await page.context().addCookies([
    { name: "doener_theme", value: theme, url: page.url() || "http://localhost" },
  ]);
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
}

/** Runs axe and fails on anything serious. */
async function scan(page: Page, where: string): Promise<void> {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();

  const serious = results.violations.filter(
    (violation) => violation.impact === "serious" || violation.impact === "critical",
  );

  const described = serious.map(
    (violation) =>
      `${violation.id} (${violation.impact ?? "unknown"}): ${violation.help}\n` +
      violation.nodes
        .slice(0, 3)
        .map((node) => `    ${node.target.join(" ")}`)
        .join("\n"),
  );

  expect(described, `${where} has serious accessibility violations`).toEqual([]);
}

test.describe("every page passes axe in both themes", () => {
  test("the public pages", async ({ page }) => {
    for (const path of ["/", "/restaurants", "/version", "/imprint", "/legal-notes", "/account"]) {
      await page.goto(path);
      for (const theme of THEMES) {
        await useTheme(page, theme);
        await scan(page, `${path} in the ${theme} theme`);
      }
    }
  });

  test("the pages a logged-in visitor sees", async ({ page, request }) => {
    const account = await register(request, "axe");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    await loginThroughTheForm(page, account);

    // An order with an item of this visitor's own, so the item list, the
    // totals and the summary all have something in them.
    await page.goto(`/orders/${orderID}`);
    await page.getByRole("button", { name: "Add an item" }).first().click();
    await page.getByRole("dialog").getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);

    for (const path of [
      "/",
      "/orders/new",
      `/orders/${orderID}`,
      `/orders/${orderID}/summary`,
      "/restaurants",
      `/restaurants/${fixture.restaurantID}`,
      "/account",
    ]) {
      await page.goto(path);
      for (const theme of THEMES) {
        await useTheme(page, theme);
        await scan(page, `${path} in the ${theme} theme`);
      }
    }
  });

  test("the dialogs, which are their own kind of page", async ({ page, request }) => {
    const account = await register(request, "axemodal");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    await loginThroughTheForm(page, account);

    // The add-item dialog.
    await page.goto(`/orders/${orderID}`);
    await page.getByRole("button", { name: "Add an item" }).first().click();
    await expect(page.getByRole("dialog")).toBeVisible();
    for (const theme of THEMES) {
      await useTheme(page, theme);
      // The reload closed the dialog, so it is opened again in the new theme.
      await page.getByRole("button", { name: "Add an item" }).first().click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await scan(page, `the add-item dialog in the ${theme} theme`);
    }

    // The menu item editor, which is the largest form in the application.
    await page.goto(`/restaurants/${fixture.restaurantID}`);
    await page.getByRole("button", { name: "Edit" }).first().click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await scan(page, "the menu item editor");
  });
});
