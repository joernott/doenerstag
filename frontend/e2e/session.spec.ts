// What a browser sees when the session it is holding has gone away.
//
// This needs a real browser and a real cookie jar. The bug it exists to prevent
// was invisible to every other kind of test: the server was answering exactly
// what its unit tests asked it to, and the damage only appeared once a browser
// attached the dead cookie to a page navigation and got a JSON error envelope
// where the application should have been.

import { expect, test } from "@playwright/test";

import { login, loginThroughTheForm, register } from "./support";

test.describe("a session the server has forgotten", () => {
  test("still loads the application, and says why once", async ({ page, request, browser }) => {
    const account = await register(request, "stale");
    await loginThroughTheForm(page, account);
    await expect(page.getByRole("link", { name: account.displayName })).toBeVisible();

    // A second login elsewhere supersedes the first: `session` has a unique
    // constraint on user_id, so this browser's row is deleted underneath it.
    // The same state arrives by logging out on another device, or by the
    // nightly cleanup, and this is the one way to reach it from a test.
    const elsewhere = await browser.newContext({ ignoreHTTPSErrors: true });
    await login(elsewhere.request, account);

    // The navigation that used to produce raw JSON.
    await page.goto("/");

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("You have been logged out");

    // 2003 rather than a generic message: being logged in somewhere else is
    // worth noticing, and is not the same as a session that merely lapsed.
    await expect(dialog).toContainText("logged in somewhere else");

    // Behind the dialog is a working application, anonymous. This is the part
    // that was broken: the page, its assets and every public endpoint were all
    // being refused because of a cookie nobody chose to send.
    await expect(page.getByRole("link", { name: "Log in / Register" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Orders" })).toBeVisible();

    // Dismissing leaves that application usable.
    await page.getByRole("button", { name: "Continue as a guest" }).click();
    await expect(dialog).toBeHidden();

    // And it is said once. A reload announces nothing, because the server
    // cleared the note when it handed it over.
    await page.reload();
    await expect(page.getByRole("heading", { name: "Orders" })).toBeVisible();
    await expect(page.getByRole("dialog")).toBeHidden();

    await elsewhere.close();
  });

  test("offers a login that works", async ({ page, request, browser }) => {
    const account = await register(request, "stale");
    await loginThroughTheForm(page, account);

    const elsewhere = await browser.newContext({ ignoreHTTPSErrors: true });
    await login(elsewhere.request, account);

    await page.goto("/");
    await page.getByRole("button", { name: "Log in", exact: true }).click();

    // The dialog's own button, not a link in the chrome: the point of offering
    // it is that somebody who was working can get back to what they were doing
    // without hunting for the way in.
    await expect(page).toHaveURL(/\/account$/);
    await expect(page.getByRole("dialog")).toBeHidden();

    await loginThroughTheForm(page, account);
    await expect(page.getByRole("link", { name: account.displayName })).toBeVisible();

    await elsewhere.close();
  });
});
