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
  // A long timeout, because an axe scan is proportional to the size of the
  // document and these pages have no fixed size: the order overview draws a
  // tile per order, and a development database that has been used for a while
  // has hundreds. CI starts from an empty one and finishes each scan in a
  // couple of seconds; the same test against a well-used database took over a
  // minute and failed on the clock rather than on a violation.
  //
  // The number is deliberately far above what any of this needs. This test
  // looks for accessibility violations and has no business also being a
  // performance assertion -- that is what the checks in internal/api measure,
  // against a known amount of data.
  test.describe.configure({ timeout: 180_000 });

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

/**
 * The WCAG 2.1 contrast ratio between two colours, each as the browser reports
 * a computed style: `rgb(r, g, b)`.
 */
function contrastRatio(foreground: string, background: string): number {
  const luminance = (colour: string): number => {
    const parts = colour.match(/\d+(\.\d+)?/g);
    if (!parts || parts.length < 3) {
      throw new Error(`cannot read the colour ${colour}`);
    }
    const channels = parts.slice(0, 3).map((value) => {
      const srgb = Number(value) / 255;
      return srgb <= 0.03928 ? srgb / 12.92 : Math.pow((srgb + 0.055) / 1.055, 2.4);
    }) as [number, number, number];
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
  };

  const a = luminance(foreground);
  const b = luminance(background);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

/** What the browser actually paints for an element right now. */
async function paintedColours(page: Page, selector: string): Promise<[string, string]> {
  return page.evaluate((sel) => {
    const element = document.querySelector(sel);
    if (!element) {
      throw new Error(`no ${sel} on the page`);
    }
    const style = getComputedStyle(element);
    return [style.color, style.backgroundColor] as [string, string];
  }, selector);
}

// A button has to stay readable while the pointer is on it.
//
// axe cannot answer this: it scans a page at rest, and nothing is hovered in a
// page at rest. The generic `.button:hover` rule takes the background away, and
// it outranks `.button-primary`, so for fourteen sprints hovering a primary
// button left its label set to a colour chosen to sit on the accent -- against
// the sunken surface instead. Near-black on near-black in the dark theme, white
// on light grey in the light one. Reported by the user looking at the running
// application, which is the only way it was ever going to be found.
test.describe("a coloured button stays readable under the pointer", () => {
  for (const theme of THEMES) {
    test(`the primary button in the ${theme} theme`, async ({ page }) => {
      await page.goto("/account");
      await useTheme(page, theme);

      const selector = "button.button-primary";
      const target = page.locator(selector).first();
      await expect(target).toBeVisible();

      const [restText, restBackground] = await paintedColours(page, selector);
      expect(
        contrastRatio(restText, restBackground),
        `at rest the label is ${restText} on ${restBackground}`,
      ).toBeGreaterThanOrEqual(4.5);

      await target.hover();

      const [hoverText, hoverBackground] = await paintedColours(page, selector);
      expect(
        contrastRatio(hoverText, hoverBackground),
        `hovered the label is ${hoverText} on ${hoverBackground}`,
      ).toBeGreaterThanOrEqual(4.5);
    });
  }
});
