// Keyboard-only operation.
//
// docs/06_ui_ux.md: every function is reachable and operable by keyboard alone,
// no control is mouse-only, modals trap focus and return it, and the skip link
// is the first focusable element. axe cannot check any of that -- it is about
// what happens when keys are pressed, not about what the markup says -- so it
// is checked here, by pressing keys.

import { expect, test, type Page } from "@playwright/test";

import { login, loginThroughTheForm, register, seedOrder, seedRestaurant } from "./support";

/** What currently has focus, as a person would describe it. */
async function focused(page: Page): Promise<string> {
  return page.evaluate(() => {
    const element = document.activeElement;
    if (!element) {
      return "nothing";
    }
    const name =
      element.getAttribute("aria-label") ??
      element.textContent?.trim().slice(0, 40) ??
      "";
    return `${element.tagName.toLowerCase()}:${name}`;
  });
}

/** Presses Tab until something matches, or gives up. Returns what it found. */
async function tabUntil(page: Page, matches: (description: string) => boolean): Promise<string> {
  for (let step = 0; step < 40; step++) {
    await page.keyboard.press("Tab");
    const description = await focused(page);
    if (matches(description)) {
      return description;
    }
  }
  return "";
}

test.describe("the keyboard alone", () => {
  test("reaches the content past the chrome with one key", async ({ page }) => {
    await page.goto("/");

    await page.keyboard.press("Tab");
    expect(await focused(page)).toContain("Skip to the content");

    await page.keyboard.press("Enter");
    // The skip link moves focus to the main element, which is where a person
    // wanted to be rather than four controls into the title bar.
    const landed = await page.evaluate(() => document.activeElement?.id ?? "");
    expect(landed).toBe("main");
  });

  test("opens the main menu, walks it and closes it", async ({ page }) => {
    await page.goto("/");

    const found = await tabUntil(page, (description) => description.includes("Open the menu"));
    expect(found).toContain("Open the menu");

    await page.keyboard.press("Enter");
    // Opening moves focus into the menu, so the next thing pressed acts on it.
    expect(await focused(page)).toContain("Orders");

    await page.keyboard.press("Escape");
    // Escape closes it and hands focus back to the button that opened it, which
    // is where the person was.
    expect(await focused(page)).toContain("Open the menu");
    await expect(page.locator("#main-menu")).toBeHidden();
  });

  test("changes the language and the theme without a mouse", async ({ page }) => {
    await page.goto("/");

    const select = page.getByLabel("Language");
    await select.focus();
    await select.selectOption("de");
    await expect(page.locator("html")).toHaveAttribute("lang", "de");

    const toggle = page.getByRole("button", { name: /dunkel|hell/i });
    await toggle.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  });

  test("adds an item to an order entirely by keyboard", async ({ page, request }) => {
    const account = await register(request, "keys");
    await login(request, account);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);

    await loginThroughTheForm(page, account);
    await page.goto(`/orders/${orderID}`);

    const add = await tabUntil(page, (description) => description.includes("Add an item"));
    expect(add).toContain("Add an item");
    await page.keyboard.press("Enter");

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    // The dialog puts focus inside itself, so the person is where the work is.
    expect(await focused(page)).not.toBe("nothing");

    // Tab to the quantity, type, then tab on to Save and press it.
    const quantity = await tabUntil(page, (description) => description.startsWith("input:"));
    expect(quantity).not.toBe("");
    await page.keyboard.type("2");

    const save = await tabUntil(page, (description) => description.includes("Save"));
    expect(save).toContain("Save");
    await page.keyboard.press("Enter");

    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText(fixture.itemName).first()).toBeVisible();
  });

  test("keeps Tab inside a modal and gives focus back when it closes", async ({ page, request }) => {
    const account = await register(request, "trap");
    await loginThroughTheForm(page, account);
    await page.goto("/account");

    const opener = page.getByRole("button", { name: "Delete my account" });
    await opener.focus();
    await page.keyboard.press("Enter");

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    // Twenty tabs cannot leave the dialog. Without a trap they would be well
    // into the title bar by now.
    for (let step = 0; step < 20; step++) {
      await page.keyboard.press("Tab");
      const inside = await page.evaluate(() =>
        document.querySelector(".modal")?.contains(document.activeElement),
      );
      expect(inside).toBe(true);
    }

    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(opener).toBeFocused();
  });

  test("reorders menu categories without dragging anything", async ({ page, request }) => {
    const account = await register(request, "reorder");
    await login(request, account);
    const fixture = await seedRestaurant(request);

    await loginThroughTheForm(page, account);
    await page.goto(`/restaurants/${fixture.restaurantID}`);

    // A second category, so there is something to reorder. Through the dialog,
    // which is where adding one lives now.
    await page.getByRole("button", { name: "Add a category" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Category").fill("Getränke");
    await dialog.getByRole("button", { name: "Create" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Move up" }).nth(1)).toBeEnabled();

    // The names are headings rather than text boxes now, so this reads them
    // rather than reading input values.
    const names = (): Promise<string[]> => page.locator(".menu-group-title").allTextContents();
    const before = await names();
    expect(before.length).toBeGreaterThan(1);

    // The reordering control is a button, not a drag handle, so it is operable
    // by keyboard by construction (docs/06_ui_ux.md).
    const moveUp = page.getByRole("button", { name: "Move up" }).nth(1);
    await moveUp.focus();
    await page.keyboard.press("Enter");

    await expect.poll(names).not.toEqual(before);
  });
});
