// The live update stream.
//
// jsdom has no EventSource, so the tests supply one. That is not a workaround:
// the factory exists in the production code precisely so that this behaviour --
// which events are listened for, what a reconnect means, when the stream gives
// up -- can be asserted without a browser.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ORDER_EVENTS, subscribeToOrder } from "../src/events";

/** The three readyState values, as the specification numbers them. */
const CONNECTING = 0;
const OPEN = 1;
const CLOSED = 2;

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readyState = CONNECTING;
  closed = false;
  private readonly listeners = new Map<string, ((event: Event) => void)[]>();

  constructor(readonly url: string) {
    FakeEventSource.instances.push(this);
  }

  addEventListener(name: string, listener: (event: Event) => void): void {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), listener]);
  }

  close(): void {
    this.closed = true;
    this.readyState = CLOSED;
  }

  /** Pretends the connection came up. */
  open(): void {
    this.readyState = OPEN;
    this.fire("open", new Event("open"));
  }

  /** Pretends an event arrived. */
  send(name: string, data: unknown): void {
    this.sendRaw(name, JSON.stringify(data));
  }

  /** Pretends an event arrived carrying exactly this text. */
  sendRaw(name: string, data: string): void {
    this.fire(name, { data } as unknown as Event);
  }

  /** Pretends the connection dropped for good. */
  fail(): void {
    this.readyState = CLOSED;
    this.fire("error", new Event("error"));
  }

  private fire(name: string, event: Event): void {
    for (const listener of this.listeners.get(name) ?? []) {
      listener(event);
    }
  }
}

const factory = (url: string): EventSource =>
  new FakeEventSource(url) as unknown as EventSource;

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

function latest(): FakeEventSource {
  const found = FakeEventSource.instances[FakeEventSource.instances.length - 1];
  if (!found) {
    throw new Error("no EventSource was created");
  }
  return found;
}

describe("subscribing", () => {
  it("opens the order's stream", () => {
    const stream = subscribeToOrder("order-1", { onEvent: () => {} }, factory);
    expect(latest().url).toBe("/api/v1/orders/order-1/events");
    stream.close();
  });

  it("listens for every documented event", () => {
    const received: string[] = [];
    const stream = subscribeToOrder("o", { onEvent: (name) => received.push(name) }, factory);

    latest().open();
    for (const name of ORDER_EVENTS) {
      latest().send(name, { id: "x" });
    }

    expect(received).toEqual([...ORDER_EVENTS]);
    stream.close();
  });

  it("parses the payload", () => {
    const payloads: unknown[] = [];
    const stream = subscribeToOrder("o", { onEvent: (_, data) => payloads.push(data) }, factory);

    latest().open();
    latest().send("order.item_count", { item_count: 9 });

    expect(payloads).toEqual([{ item_count: 9 }]);
    stream.close();
  });

  it("survives a payload it cannot parse", () => {
    const received: [string, unknown][] = [];
    const stream = subscribeToOrder("o", { onEvent: (name, data) => received.push([name, data]) }, factory);

    latest().open();
    // A truncated frame is not worth tearing the stream down for: the page is
    // told that something happened and can fetch if it needs the detail.
    latest().sendRaw("item.created", "{not json");

    expect(received).toEqual([["item.created", null]]);
    stream.close();
  });
});

describe("when the connection drops", () => {
  it("does not call onReconnect for the first connection", () => {
    const reconnected = vi.fn();
    const stream = subscribeToOrder("o", { onEvent: () => {}, onReconnect: reconnected }, factory);

    latest().open();
    expect(reconnected).not.toHaveBeenCalled();
    stream.close();
  });

  it("reconnects, and says so, because what happened in between was missed", () => {
    const reconnected = vi.fn();
    const stream = subscribeToOrder("o", { onEvent: () => {}, onReconnect: reconnected }, factory);

    latest().open();
    latest().fail();

    // Nothing yet: the retry is deliberately a beat later.
    expect(FakeEventSource.instances).toHaveLength(1);

    vi.advanceTimersByTime(3000);
    expect(FakeEventSource.instances).toHaveLength(2);

    latest().open();
    expect(reconnected).toHaveBeenCalledTimes(1);
    stream.close();
  });

  it("stops retrying once it is closed", () => {
    const stream = subscribeToOrder("o", { onEvent: () => {} }, factory);
    latest().open();
    latest().fail();

    stream.close();
    vi.advanceTimersByTime(10000);

    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("closes the underlying source, twice if asked", () => {
    const stream = subscribeToOrder("o", { onEvent: () => {} }, factory);
    latest().open();

    stream.close();
    expect(latest().closed).toBe(true);
    expect(() => stream.close()).not.toThrow();
  });
});
