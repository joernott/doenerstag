// The order page.
//
// Two columns at the large breakpoint and stacked below: the order and its
// items on the left, the restaurant's menu on the right. The menu is public;
// the item list is not, and an anonymous visitor is not shown a blurred or
// disabled version of it -- the API never sends it, and the page does not draw
// it (F1.2).
//
// Everything that changes while the page is open arrives on one SSE stream
// (F7.1): somebody else's item, an edit to the order, the deadline passing.
//
// This module loads the order, lays the page out and keeps it live. What is
// drawn is in the modules beside it, which share an OrderContext: details.ts
// (the order's facts and its own controls), items.ts (what has been ordered)
// and menu.ts (the menu column).

import type { App } from "../../app";
import { api, getList } from "../../api";
import { el, rerender, type Child } from "../../dom";
import {
  EVENT_ORDER_DELETED,
  EVENT_ORDER_EXPIRED,
  subscribeToOrder,
  type EventSourceFactory,
} from "../../events";
import { orderTitle } from "../../format";
import { moneyFormatOf, referenceData } from "../../reference";
import { statusLine } from "../../components/status";
import type { Category, MenuItem, OrderDetail, OrderHeader, RestaurantDetail } from "../../types";
import { failedPage, pageWithActions, section } from "../page";
import { menuPath, orderContext } from "./context";
import { orderDetails } from "./details";
import { orderItems } from "./items";
import { orderMenu } from "./menu";

export interface OrderPageOptions {
  /**
   * Registers the stream to be closed when this page is replaced.
   *
   * The router supplies it. Without one the stream would outlive the page, and
   * with a router-wide one the render that installs the page would close the
   * stream it has just opened.
   */
  cleanup?: { onCleanup: (undo: () => void) => void };
  /**
   * How an EventSource is made. A test supplies its own: jsdom has none, and a
   * live-update path that could only be tested in a real browser would be
   * tested rarely.
   */
  factory?: EventSourceFactory;
}

/** The page for one order. */
export async function orderPage(
  app: App,
  id: string,
  options: OrderPageOptions = {},
): Promise<HTMLElement> {
  const factory = options.factory;
  const { t } = app;

  let order: OrderHeader | OrderDetail;
  try {
    order = await api.get<OrderHeader | OrderDetail>(`/orders/${id}`);
  } catch (error) {
    return failedPage(t.t("nav.orders"), t, error);
  }

  const [reference, restaurant, categories, menu] = await Promise.all([
    referenceData().catch(() => null),
    api.get<RestaurantDetail>(`/restaurants/${order.restaurant_id}`).catch(() => null),
    getList<Category>(`/restaurants/${order.restaurant_id}/categories`, "categories").catch(
      () => [],
    ),
    getList<MenuItem>(menuPath(order), "menu_items").catch(() => []),
  ]);

  const left = el("div", { class: "order-column" });
  const right = el("div", { class: "order-column" });
  const banner = el("div");
  const heading = el("div", { class: "page-heading-actions" });

  const ctx = orderContext({
    app,
    id,
    order,
    reference,
    restaurant,
    moneyFormat: moneyFormatOf(reference?.currencies ?? [], order.currency_code),
    status: statusLine(),
    menuItems: menu,
    categoryList: categories,
    render: () => {
      renderLeft();
      menuColumn.renderMenu();
    },
    renderMenu: () => {
      menuColumn.renderMenu();
    },
  });

  const details = orderDetails(ctx, heading);
  const items = orderItems(ctx);
  const menuColumn = orderMenu(ctx, right, items.openItemEditor);

  // The left column: the order's own card, then what has been ordered -- or,
  // for an anonymous visitor, only how much.
  function renderLeft(): void {
    details.renderHeading();

    const own = ctx.detail();
    const parts: Child[] = [details.orderCard()];
    if (own) {
      parts.push(items.itemsCard(own), items.totalsCard(own));
    } else {
      parts.push(items.anonymousCard());
    }

    rerender(left, ...parts);
    banner.replaceChildren(
      ctx.active()
        ? el("span", { hidden: true })
        : el("p", {
            class: "notice",
            role: "status",
            text: t.t("order.closed_notice"),
          }),
    );
  }

  // --- live updates --------------------------------------------------------

  // One set of handlers for every subscription this page makes, including the
  // one made after logging in or out: a bare re-fetch there would leave a
  // deleted order on screen and announce nothing.
  const handlers = {
    onEvent: (name: string): void => {
      if (name === EVENT_ORDER_DELETED) {
        // Nothing left to look at: say so and go back to the overview.
        app.announce(t.t("order.deleted"));
        left.replaceChildren(
          section(orderTitle(app.language, ctx.order), el("p", { text: t.t("order.deleted") })),
        );
        stream.close();
        return;
      }

      // Every other event is answered by re-fetching. The payloads carry
      // enough to patch the DOM in place, and doing so would mean a second
      // implementation of "what an order looks like" that could drift from
      // the first. One fetch on a local network is cheaper than that risk.
      void ctx.refresh().then(() => {
        app.announce(
          name === EVENT_ORDER_EXPIRED ? t.t("order.closed_notice") : t.t("order.live.changed"),
        );
      });
    },
    onReconnect: (): void => {
      // Whatever happened while the stream was down was never delivered.
      void ctx.refresh();
    },
  };

  let stream = subscribeToOrder(id, handlers, factory);

  // Logging in or out changes what the stream is allowed to send, so the
  // subscription is made again -- and the page is re-fetched, because the shape
  // of the answer changes too.
  const unsubscribe = app.session.subscribe(() => {
    stream.close();
    stream = subscribeToOrder(id, handlers, factory);
    void ctx.refresh();
  });

  // A stream outlives the DOM it feeds unless somebody closes it, and this page
  // cannot see that it has been navigated away from. The router can, so it is
  // the router that says when.
  options.cleanup?.onCleanup(() => {
    stream.close();
    unsubscribe();
  });

  renderLeft();
  menuColumn.renderMenu();

  return pageWithActions(
    orderTitle(app.language, ctx.order),
    heading,
    banner,
    el("div", { class: "order-layout" }, left, right),
  );
}
