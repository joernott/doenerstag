// The live update stream.
//
// One EventSource per open order (ADR-0003). The browser reconnects on its own
// when a connection drops, but it does not tell the page that it did -- and
// everything that happened while the stream was down is simply missing. So a
// reconnect is treated as "I have been away": the page re-fetches, and the
// stream carries on from a state that is known to be current.
//
// What arrives depends on who is asking. A logged-in subscriber receives the
// item events; an anonymous one receives the header events and a running item
// count, and never any item detail (F7.4). That split is the server's, and this
// module does not attempt to reproduce it -- it delivers what arrives.

/** The event names from docs/04_api.md. */
export const EVENT_ITEM_CREATED = "item.created";
export const EVENT_ITEM_UPDATED = "item.updated";
export const EVENT_ITEM_DELETED = "item.deleted";
export const EVENT_ITEM_COUNT = "order.item_count";
export const EVENT_ORDER_UPDATED = "order.updated";
export const EVENT_ORDER_DELETED = "order.deleted";
export const EVENT_ORDER_EXPIRED = "order.expired";

/** Every event the stream sends that a page acts on. */
export const ORDER_EVENTS = [
  EVENT_ITEM_CREATED,
  EVENT_ITEM_UPDATED,
  EVENT_ITEM_DELETED,
  EVENT_ITEM_COUNT,
  EVENT_ORDER_UPDATED,
  EVENT_ORDER_DELETED,
  EVENT_ORDER_EXPIRED,
] as const;

export interface StreamHandlers {
  /** An event arrived. The payload is already parsed. */
  onEvent: (name: string, data: unknown) => void;
  /**
   * The stream came back after a drop.
   *
   * Called on every reconnection and not on the first connection, because the
   * page has just fetched everything anyway. Whatever happened while the
   * connection was down was never delivered, so the page has to fetch again.
   */
  onReconnect?: () => void;
}

/** A subscription. Closing it is idempotent. */
export interface Stream {
  close(): void;
  /** Whether the stream is currently open, for the tests. */
  readonly connected: boolean;
}

/**
 * How an EventSource is made.
 *
 * Injectable so a test can supply its own. jsdom has no EventSource at all, and
 * a page that could only be tested in a real browser would be tested rarely.
 */
export type EventSourceFactory = (url: string) => EventSource;

const defaultFactory: EventSourceFactory = (url) => new EventSource(url);

/** Subscribes to one order's stream. */
export function subscribeToOrder(
  orderID: string,
  handlers: StreamHandlers,
  factory: EventSourceFactory = defaultFactory,
): Stream {
  let source: EventSource | null = null;
  let closed = false;
  // The first `open` is the subscription itself, not a recovery.
  let everOpened = false;

  const url = `/api/v1/orders/${encodeURIComponent(orderID)}/events`;

  const dispatch = (name: string, event: MessageEvent<string>): void => {
    let data: unknown = null;
    try {
      data = JSON.parse(event.data);
    } catch {
      // A malformed payload is not worth tearing the stream down for; the page
      // is told about the event and can re-fetch if it needs the detail.
      data = null;
    }
    handlers.onEvent(name, data);
  };

  const connect = (): void => {
    source = factory(url);

    source.addEventListener("open", () => {
      if (everOpened) {
        handlers.onReconnect?.();
      }
      everOpened = true;
    });

    for (const name of ORDER_EVENTS) {
      source.addEventListener(name, (event) => {
        dispatch(name, event as MessageEvent<string>);
      });
    }

    // EventSource reconnects by itself, so an error is only worth noting when
    // it has given up: readyState CLOSED means it will not try again, and only
    // then does this module make a new one.
    source.addEventListener("error", () => {
      if (closed || !source) {
        return;
      }
      if (source.readyState === 2) {
        source.close();
        source = null;
        retry();
      }
    });
  };

  let timer: ReturnType<typeof setTimeout> | null = null;
  const retry = (): void => {
    if (closed || timer !== null) {
      return;
    }
    // A fixed short delay rather than a backoff: the server this talks to is on
    // the same network, and the case being handled is a restart, which is over
    // in seconds. A backoff would mostly add waiting to a page that is already
    // showing stale data.
    timer = setTimeout(() => {
      timer = null;
      if (!closed) {
        connect();
      }
    }, 3000);
  };

  connect();

  return {
    close(): void {
      closed = true;
      if (timer !== null) {
        clearTimeout(timer);
        timer = null;
      }
      source?.close();
      source = null;
    },
    get connected(): boolean {
      return source !== null && source.readyState === 1;
    },
  };
}
