// The order overview: the start page.
//
// A grid of tiles whose first entry opens a new order. Active orders first,
// then expired ones faded but fully clickable -- and the server has already put
// them in that order, so this page does not sort again.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el, type Child } from "../dom";
import { formatDateTime, formatMoney, formatRelativeTime } from "../format";
import { thumbnailURL } from "../components/images";
import { addTile, tile, tileGrid } from "../components/tiles";
import { minorUnitOf, referenceData } from "../reference";
import { page } from "./page";

/** The order header, which every caller receives. */
export interface OrderHeader {
  id: string;
  title: string;
  restaurant_id: string;
  restaurant_name: string;
  restaurant_logo_image_id: string | null;
  fulfilment: string;
  fulfilment_at: string;
  deadline_at: string;
  status: string;
  money_collector: string;
  pickup_person: string;
  currency_code: string;
  min_order_value_cents: number | null;
  delivery_fee_cents: number | null;
  item_count: number;
}

/** The list entry a logged-in caller receives: the header plus who opened it. */
export interface OrderListEntry extends OrderHeader {
  creator_id?: string;
  creator_name?: string;
}

/** One order item, which only an authenticated caller ever sees. */
export interface OrderItem {
  id: string;
  user_id: string;
  user_name: string;
  menu_item_id: string;
  quantity: number;
  item_name: string;
  unit_price_cents: number;
  note: string;
  modifications: { id: string; modification_id: string | null; name: string; price_delta_cents: number }[];
  line_total_cents: number;
}

/** The authenticated order shape: the header, the creator and the items. */
export interface OrderDetail extends OrderHeader {
  creator_id: string;
  creator_name: string;
  items: OrderItem[];
  item_total_cents: number;
  grand_total_cents: number;
  below_minimum: boolean;
}

/** Whether an order is still open, judged on the viewer's clock. */
export function isActive(order: OrderHeader, now: Date = new Date()): boolean {
  return new Date(order.deadline_at).getTime() > now.getTime();
}

export async function ordersPage(app: App): Promise<HTMLElement> {
  const { t } = app;

  let orders: OrderListEntry[];
  try {
    orders = await getList<OrderListEntry>("/orders", "orders");
  } catch (error) {
    return page(t.t("nav.orders"), el("p", { class: "field-error", text: errorMessage(t, error) }));
  }

  // The tile shows a participant count and a total for a logged-in visitor, and
  // the list carries neither -- it carries no item data for anybody
  // (ADR-0011). They are fetched per order, in parallel, and only when there is
  // somebody entitled to see them. The alternative would be a second, aggregate
  // total computed in SQL, which could then disagree with the one on the order
  // page; one arithmetic is worth several requests at this scale.
  const details = app.session.isAuthenticated
    ? await Promise.all(
        orders.map((order) =>
          api.get<OrderDetail>(`/orders/${order.id}`).catch(() => null),
        ),
      )
    : orders.map(() => null);

  const currencies = await referenceData()
    .then((reference) => reference.currencies)
    .catch(() => []);

  const create = app.session.isAuthenticated ? "/orders/new" : "/account";
  const tiles = orders.map((order, index) =>
    orderTile(app, order, details[index] ?? null, minorUnitOf(currencies, order.currency_code)),
  );

  return page(t.t("nav.orders"), tileGrid(addTile(create, t.t("order.new")), ...tiles));
}

function orderTile(
  app: App,
  order: OrderListEntry,
  detail: OrderDetail | null,
  minorUnit: number,
): HTMLElement {
  const { t } = app;
  const active = isActive(order);
  const parts: Child[] = [];

  parts.push(
    logo(order),
    el("strong", { class: "tile-title", text: order.title }),
    el("span", {
      class: "muted",
      text: `${t.t(
        order.fulfilment === "delivery" ? "order.fulfilment.delivery" : "order.fulfilment.pickup",
      )} · ${formatDateTime(app.language, order.fulfilment_at)}`,
    }),
  );

  // The deadline, with a relative hint while the order is still open: "in 2
  // hours" is what somebody deciding whether to join actually needs.
  parts.push(
    el(
      "span",
      { class: "muted" },
      `${t.t("order.deadline.label")}: ${formatDateTime(app.language, order.deadline_at)}`,
      el("span", {
        class: "tile-hint",
        text: ` (${formatRelativeTime(app.language, order.deadline_at)})`,
      }),
    ),
  );

  // The item count is shown to everyone; it is not item data (F1.2).
  parts.push(el("span", { text: t.t("order.items", { count: order.item_count }) }));

  if (detail) {
    const participants = new Set(detail.items.map((item) => item.user_id));
    parts.push(
      el("span", { class: "muted", text: t.t("order.participants", { count: participants.size }) }),
      el("span", {
        text: formatMoney(app.language, detail.grand_total_cents, order.currency_code, minorUnit),
      }),
    );
  }

  if (order.creator_name) {
    parts.push(
      el("span", { class: "muted", text: `${t.t("order.creator")}: ${order.creator_name}` }),
    );
  }

  // Faded expired orders also say so in words: opacity is not information.
  if (!active) {
    parts.push(el("span", { class: "badge", text: t.t("order.status.expired") }));
  }

  // The summary, for participants only (F1.3), and outside the tile's link
  // rather than inside it: a link within a link is not a thing a browser can
  // make sense of. Expired orders keep it -- the summary is what somebody
  // settling up afterwards actually wants.
  const me = app.session.user?.id;
  const participant =
    detail !== null &&
    me !== undefined &&
    (app.session.isAdmin ||
      detail.creator_id === me ||
      detail.items.some((item) => item.user_id === me));

  return tile(
    {
      href: `/orders/${order.id}`,
      faded: !active,
      ...(participant
        ? {
            footer: el(
              "div",
              { class: "tile-footer" },
              el("a", {
                class: "button button-quiet",
                href: `/orders/${order.id}/summary`,
                text: t.t("order.summary"),
              }),
            ),
          }
        : {}),
    },
    ...parts,
  );
}

function logo(order: OrderHeader): HTMLElement {
  if (!order.restaurant_logo_image_id) {
    return el("div", { class: "tile-logo tile-logo-empty", "aria-hidden": "true" });
  }
  return el("img", {
    class: "tile-logo",
    src: thumbnailURL(order.restaurant_logo_image_id),
    alt: "",
    loading: "lazy",
  });
}
