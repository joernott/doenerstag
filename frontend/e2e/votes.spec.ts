// Votes (F12), in two real browsers: one person starts a vote through the
// plus tile, another votes, and the first sees the count and the lead move
// without a reload.

import {
  expect,
  login,
  loginThroughTheForm,
  register,
  seedRestaurant,
  seedVote,
  steady,
  test,
} from "./support";

test.describe("a vote for the restaurant", () => {
  test("is started from the plus tile and followed live", async ({ browser, request }) => {
    const host = await register(request, "votehost");
    await login(request, host);
    const first = await seedRestaurant(request);
    const second = await seedRestaurant(request);
    const guest = await register(request, "voteguest");

    const hostContext = await browser.newContext();
    const guestContext = await browser.newContext();
    const hosting = steady(await hostContext.newPage());
    const voting = steady(await guestContext.newPage());

    // The plus tile asks what to start.
    await loginThroughTheForm(hosting, host);
    await hosting.goto("/");
    await hosting.getByRole("link", { name: "New order or vote" }).click();
    const choice = hosting.getByRole("dialog");
    await expect(choice.getByRole("link", { name: /An order/ })).toBeVisible();
    await choice.getByRole("link", { name: /A vote/ }).click();

    // Two restaurants, the default times, public ballots.
    await expect(hosting).toHaveURL(/\/votes\/new$/);
    await hosting.getByLabel(first.restaurantName).check();
    await hosting.getByLabel(second.restaurantName).check();
    await hosting.getByLabel("Ballots are public").check();
    await hosting.getByRole("button", { name: "New vote" }).click();
    await expect(hosting).toHaveURL(/\/votes\/[0-9a-f-]{36}$/);
    const voteURL = new URL(hosting.url()).pathname;
    await expect(hosting.getByText("No votes yet").first()).toBeVisible();

    // The guest votes for the second restaurant.
    await loginThroughTheForm(voting, guest);
    await voting.goto(voteURL);
    await voting.getByRole("radio", { name: new RegExp(second.restaurantName) }).check();
    await expect(voting.locator(".vote-total")).toHaveText("1 vote");

    // The host's page follows without a reload: the count, the total, the
    // lead in words and, with public ballots, the guest's name.
    const ahead = hosting.locator(".vote-row-ahead");
    await expect(ahead).toContainText(second.restaurantName);
    await expect(ahead).toContainText("Ahead");
    await expect(ahead).toContainText(guest.displayName);
    await expect(hosting.locator(".vote-total")).toHaveText("1 vote");

    // The host votes for the first: level, and the second got there first.
    await hosting.getByRole("radio", { name: new RegExp(first.restaurantName) }).check();
    await expect(hosting.locator(".vote-total")).toHaveText("2 votes");
    await expect(hosting.locator(".vote-row-ahead")).toContainText(second.restaurantName);

    // The guest changes their mind: one ballot still, and the first leads.
    await voting.getByRole("radio", { name: new RegExp(first.restaurantName) }).check();
    await expect(voting.locator(".vote-total")).toHaveText("2 votes");
    await expect(hosting.locator(".vote-row-ahead")).toContainText(first.restaurantName);

    // The vote is on the start page, marked as one.
    await hosting.goto("/");
    await expect(hosting.locator(".tile", { hasText: "Vote" }).first()).toBeVisible();

    await hostContext.close();
    await guestContext.close();
  });

  test("tells an anonymous visitor the counts and nobody's name", async ({ page, request }) => {
    const host = await register(request, "votesecret");
    await login(request, host);
    const first = await seedRestaurant(request);
    const second = await seedRestaurant(request);
    const voteID = await seedVote(request, [first.restaurantID, second.restaurantID], {
      public_ballots: true,
    });

    await page.goto(`/votes/${voteID}`);
    await expect(page.getByRole("radio").first()).toBeDisabled();
    await expect(page.getByText("Log in to vote")).toBeVisible();
    await expect(page.locator("body")).not.toContainText(host.displayName);
  });
});
