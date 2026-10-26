// The order chat, in two real browsers (21.8): one participant writes, the
// other sees it arrive without a reload, counted in the tab's title.

import { expect, login, loginThroughTheForm, register, seedOrder, seedRestaurant, steady, test } from "./support";

test.describe("the order chat", () => {
  test("carries a message from one participant to another without a reload", async ({
    browser,
    request,
  }) => {
    const host = await register(request, "host");
    await login(request, host);
    const fixture = await seedRestaurant(request);
    const orderID = await seedOrder(request, fixture.restaurantID);
    const guest = await register(request, "guest");

    const first = await browser.newContext();
    const second = await browser.newContext();
    const reading = steady(await first.newPage());
    const writing = steady(await second.newPage());

    // The host opened the order, so is a participant without ordering.
    await loginThroughTheForm(reading, host);
    await reading.goto(`/orders/${orderID}`);
    await reading.getByRole("tab", { name: "Chat (0)" }).click();
    await expect(reading.getByText("No messages yet.", { exact: false })).toBeVisible();

    // The guest is not, until they order something: the tab says so.
    await loginThroughTheForm(writing, guest);
    await writing.goto(`/orders/${orderID}`);
    await expect(writing.getByRole("tab", { name: "Chat (0)" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    await writing.getByRole("button", { name: "Add an item" }).first().click();
    await writing.getByRole("dialog").getByRole("button", { name: "Save" }).click();
    await expect(writing.getByRole("dialog")).toHaveCount(0);

    const tab = writing.getByRole("tab", { name: "Chat (0)" });
    await expect(tab).not.toHaveAttribute("aria-disabled", "true");
    await tab.click();
    const field = writing.getByLabel("Message", { exact: true });
    await field.fill("Ich hole das Essen um zwölf.");
    await field.press("Enter");
    await expect(field).toHaveValue("");

    // Nobody reloaded the first window.
    const line = reading.locator(".chat-line").filter({ hasText: "Ich hole das Essen um zwölf." });
    await expect(line).toBeVisible({ timeout: 15_000 });
    await expect(line.locator("strong")).toHaveText(guest.displayName);
    await expect(reading.getByRole("tab", { name: "Chat (1)" })).toBeVisible();

    await first.close();
    await second.close();
  });
});
