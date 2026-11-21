// Votes (F12): the pie, the vote page, starting a vote and the overview.

import { describe, expect, it } from "vitest";

import type { App } from "../src/app";
import { pieChart, slicePath } from "../src/components/pie";
import { ordersPage } from "../src/pages/orders";
import { votePage } from "../src/pages/vote";
import { createVotePage } from "../src/pages/votecreate";
import type { Vote } from "../src/types";
import { mountApp, settle, stubServer } from "./helpers";

const silentFactory = (): EventSource =>
  ({ addEventListener: () => {}, close: () => {}, readyState: 1 }) as unknown as EventSource;

/** An EventSource the test can push events through. */
function liveFactory(): { factory: () => EventSource; emit: (name: string, data: unknown) => void } {
  const listeners = new Map<string, ((event: MessageEvent<string>) => void)[]>();
  return {
    factory: () =>
      ({
        addEventListener: (name: string, fn: (event: MessageEvent<string>) => void) => {
          listeners.set(name, [...(listeners.get(name) ?? []), fn]);
        },
        close: () => {},
        readyState: 1,
      }) as unknown as EventSource,
    emit: (name, data) => {
      for (const fn of listeners.get(name) ?? []) {
        fn({ data: JSON.stringify(data) } as MessageEvent<string>);
      }
    },
  };
}

function signIn(app: App, id = "u1", admin = false): void {
  app.session.set({ id, name: id, display_name: id, is_admin: admin });
}

const future = (hours: number): string => new Date(Date.now() + hours * 3600_000).toISOString();

function vote(overrides: Partial<Vote> = {}): Vote {
  return {
    id: "v1",
    title: "Vote",
    decision_at: future(1),
    deadline_at: future(2),
    fulfilment_at: future(3),
    status: "open",
    open_choices: false,
    public_ballots: false,
    outcome: null,
    order_id: null,
    total: 3,
    leader_choice_id: "c2",
    choices: [
      { id: "c1", restaurant_id: "r1", restaurant_name: "Pinar Kebap", restaurant_logo_image_id: null, count: 1 },
      { id: "c2", restaurant_id: "r2", restaurant_name: "Pizzeria Roma", restaurant_logo_image_id: null, count: 2 },
    ],
    created_at: future(0),
    updated_at: future(0),
    ...overrides,
  };
}

describe("the pie", () => {
  it("draws one slice per restaurant with votes", () => {
    const svg = pieChart(
      [
        { value: 1, colour: 1, pulled: false },
        { value: 0, colour: 2, pulled: false },
        { value: 3, colour: 3, pulled: true },
      ],
      "none",
    );
    expect(svg.querySelectorAll(".vote-slice")).toHaveLength(2);
    expect(svg.querySelector(".vote-slice-3")?.getAttribute("transform")).toMatch(/^translate/);
    expect(svg.getAttribute("aria-hidden")).toBe("true");
  });

  it("is a whole circle when one restaurant has every vote", () => {
    const svg = pieChart([{ value: 4, colour: 2, pulled: true }], "none");
    expect(svg.querySelector("circle.vote-slice-2")).not.toBeNull();
  });

  it("is an empty ring with the label while nobody has voted", () => {
    const svg = pieChart([{ value: 0, colour: 1, pulled: false }], "No votes yet");
    expect(svg.querySelector(".vote-pie-empty")).not.toBeNull();
    expect(svg.textContent).toBe("No votes yet");
  });

  it("takes the long way round for a slice over half", () => {
    expect(slicePath(0, 0.75)).toContain(" 0 1 1 ");
    expect(slicePath(0, 0.25)).toContain(" 0 0 1 ");
  });
});

describe("the vote page", () => {
  it("shows each restaurant's count, the total and the leader in words", async () => {
    stubServer({ "GET /votes/v1": vote() });
    const app = mountApp(() => []);
    const page = await votePage(app, "v1", { factory: silentFactory });
    await settle();

    const counts = [...page.querySelectorAll(".vote-count")].map((node) => node.textContent);
    expect(counts).toEqual(["1", "2"]);
    expect(page.querySelector(".vote-total")?.textContent).toBe("3 votes");

    const ahead = page.querySelector(".vote-row-ahead");
    expect(ahead?.textContent).toContain("Pizzeria Roma");
    expect(ahead?.textContent).toContain("Ahead");
    expect(page.querySelectorAll(".vote-row-ahead")).toHaveLength(1);
  });

  it("lets nobody anonymous vote, and offers the login", async () => {
    stubServer({ "GET /votes/v1": vote() });
    const app = mountApp(() => []);
    const page = await votePage(app, "v1", { factory: silentFactory });
    await settle();

    const radios = [...page.querySelectorAll<HTMLInputElement>("input[type=radio]")];
    expect(radios.every((radio) => radio.disabled)).toBe(true);
    expect(page.textContent).toContain("Log in to vote");
  });

  it("casts a ballot with one click", async () => {
    const after = vote({ my_choice_id: "c1", total: 4, choices: vote().choices.map((c) => (c.id === "c1" ? { ...c, count: 2 } : c)) });
    const server = stubServer({ "GET /votes/v1": vote({ my_choice_id: null }), "PUT /votes/v1/ballot": after });
    const app = mountApp(() => []);
    signIn(app);
    const page = await votePage(app, "v1", { factory: silentFactory });
    // A radio fires its change event only from inside a document.
    document.body.appendChild(page);
    await settle();

    const first = page.querySelector<HTMLInputElement>("input[value=c1]");
    first?.click();
    await settle(10);

    expect(server.calls.find((call) => call.method === "PUT")?.body).toEqual({ choice_id: "c1" });
    expect(page.querySelector<HTMLInputElement>("input[value=c1]")?.checked).toBe(true);
    expect(page.querySelector(".vote-total")?.textContent).toBe("4 votes");
  });

  it("keeps the viewer's own choice through a live update, which never carries it", async () => {
    const live = liveFactory();
    stubServer({ "GET /votes/v1": vote({ my_choice_id: "c1" }) });
    const app = mountApp(() => []);
    signIn(app);
    const page = await votePage(app, "v1", { factory: live.factory });
    await settle();

    const shared = vote({ total: 5 });
    delete shared.my_choice_id;
    live.emit("vote.updated", shared);
    await settle();

    expect(page.querySelector<HTMLInputElement>("input[value=c1]")?.checked).toBe(true);
    expect(page.querySelector(".vote-total")?.textContent).toBe("5 votes");
  });

  it("names the voters only when the server sent them", async () => {
    const withVoters = vote({
      public_ballots: true,
      choices: vote().choices.map((c) => ({ ...c, voters: c.id === "c2" ? [{ id: "u2", name: "Berta" }, { id: "u3", name: "Carl" }] : [] })),
    });
    stubServer({ "GET /votes/v1": withVoters });
    const app = mountApp(() => []);
    signIn(app);
    const page = await votePage(app, "v1", { factory: silentFactory });
    await settle();
    expect(page.textContent).toContain("Chosen by Berta, Carl");
    expect(page.textContent).toContain("Public ballots");
  });

  it("says what the decision made and links to the order", async () => {
    stubServer({
      "GET /votes/v1": vote({ status: "decided", outcome: "ordered", order_id: "o9", decision_at: future(-1) }),
    });
    const app = mountApp(() => []);
    const page = await votePage(app, "v1", { factory: silentFactory });
    await settle();

    expect(page.textContent).toContain("Decided: Pizzeria Roma");
    expect(page.querySelector("a[href='/orders/o9']")).not.toBeNull();
    expect(page.textContent).toContain("Winner");
  });
});

describe("starting a vote", () => {
  const restaurants = {
    restaurants: [
      { id: "r1", name: "Pinar Kebap", logo_image_id: null, currency_code: "EUR" },
      { id: "r2", name: "Pizzeria Roma", logo_image_id: null, currency_code: "EUR" },
      { id: "r3", name: "Thai Garden", logo_image_id: null, currency_code: "EUR" },
    ],
  };

  it("refuses fewer than two restaurants before asking the server", async () => {
    const server = stubServer({ "GET /restaurants": restaurants, "GET /restaurants/r1": { opening_hours: [] } });
    const app = mountApp(() => []);
    signIn(app);
    const page = await createVotePage(app);
    document.body.appendChild(page);
    await settle();

    page.querySelector<HTMLInputElement>("input[value=r1]")?.click();
    page.querySelector("form")?.dispatchEvent(new Event("submit", { cancelable: true }));
    await settle();

    expect(server.calls.some((call) => call.method === "POST")).toBe(false);
    expect(page.textContent).toContain("A vote needs two to five different restaurants.");
  });

  it("sends the chosen restaurants and both switches", async () => {
    const server = stubServer({
      "GET /restaurants": restaurants,
      "GET /restaurants/r1": { opening_hours: [] },
      "GET /restaurants/r3": { opening_hours: [] },
      "POST /votes": { id: "v7" },
    });
    const app = mountApp(() => []);
    signIn(app);
    const page = await createVotePage(app);
    document.body.appendChild(page);
    await settle();

    page.querySelector<HTMLInputElement>("input[value=r1]")?.click();
    page.querySelector<HTMLInputElement>("input[value=r3]")?.click();
    page.querySelector<HTMLInputElement>("input[name=public_ballots]")?.click();
    page.querySelector("form")?.dispatchEvent(new Event("submit", { cancelable: true }));
    await settle(10);

    const post = server.calls.find((call) => call.method === "POST")?.body as Record<string, unknown>;
    expect(post["restaurant_ids"]).toEqual(["r1", "r3"]);
    expect(post["public_ballots"]).toBe(true);
    expect(post["open_choices"]).toBe(false);
  });
});

describe("the start page", () => {
  // 23.1: the plus starts either, so it says both.
  it("labels the plus tile for an order or a vote", async () => {
    stubServer({ "GET /orders": { orders: [] }, "GET /votes": { votes: [] } });
    const app = mountApp(() => []);
    const page = await ordersPage(app);
    await settle();
    expect(page.querySelector(".tile-add-label")?.textContent).toBe("New order or vote");
  });

  it("lists an open vote as a vote, with the restaurant ahead", async () => {
    stubServer({ "GET /orders": { orders: [] }, "GET /votes": { votes: [vote()] } });
    const app = mountApp(() => []);
    const page = await ordersPage(app);
    await settle();

    const tile = page.querySelector("a[href='/votes/v1']")?.closest(".tile");
    expect(tile?.textContent).toContain("Vote");
    expect(tile?.textContent).toContain("Pizzeria Roma ahead, 2 of 3");
  });

  it("asks a signed-in visitor whether to start an order or a vote", async () => {
    stubServer({ "GET /orders": { orders: [] }, "GET /votes": { votes: [] } });
    const app = mountApp(() => []);
    signIn(app);
    const page = await ordersPage(app);
    document.body.appendChild(page);
    await settle();

    page.querySelector<HTMLAnchorElement>(".tile-add-label")?.click();
    await settle();

    const dialog = document.querySelector("[role=dialog]");
    expect(dialog?.querySelector("a[href='/orders/new']")).not.toBeNull();
    expect(dialog?.querySelector("a[href='/votes/new']")).not.toBeNull();
  });
});
