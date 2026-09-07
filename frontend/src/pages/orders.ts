// The order overview: the start page.
//
// A grid of tiles whose first entry opens a new order. Active orders first,
// then expired ones faded but fully clickable -- and the server has already put
// them in that order, so this page does not sort again.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el, icon, type Child } from "../dom";
import { formatDateTime, formatRelativeTime } from "../format";
import { logoMark } from "../logo";
import { thumbnailURL } from "../components/images";
import { confirmDialog } from "../components/modal";
import { addTile, tile, tileActions, tileGrid } from "../components/tiles";
import { overviewPage, page, statusLine, type StatusLine } from "./page";

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

  // Whether this visitor takes part in each order, which decides whether the
  // tile offers its summary. The list carries no item data for anybody
  // (ADR-0011), so it cannot answer that, and the order itself is fetched per
  // entry and in parallel to find out. Affordable at the scale ADR-0006
  // assumes; the tiles no longer show a participant count or a total, so this
  // is the only thing the extra request still buys.
  const details = app.session.isAuthenticated
    ? await Promise.all(
        orders.map((order) =>
          api.get<OrderDetail>(`/orders/${order.id}`).catch(() => null),
        ),
      )
    : orders.map(() => null);

  // One status line for the page rather than one per tile: the only thing that
  // reports here is a failed deletion, and a message under the heading is where
  // somebody will look for it.
  const status = statusLine();

  const create = app.session.isAuthenticated ? "/orders/new" : "/account";
  const tiles = orders.map((order, index) =>
    orderTile(app, order, details[index] ?? null, status),
  );

  return overviewPage(
    t.t("nav.orders"),
    status.element,
    tileGrid(addTile(create, t.t("order.new")), ...tiles),
  );
}

/**
 * One order tile.
 *
 * What it shows is deliberately thin: the restaurant, when the food arrives and
 * when the order closes. The item count, the participant count, the running
 * total and who opened it all used to be here and are not any more -- they are
 * on the order itself, which is one click away, and a grid of tiles is a place
 * to choose from rather than a place to read from.
 */
function orderTile(
  app: App,
  order: OrderListEntry,
  detail: OrderDetail | null,
  status: StatusLine,
): HTMLElement {
  const { t } = app;
  const active = isActive(order);
  const parts: Child[] = [];

  parts.push(
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

  // Faded expired orders also say so in words: opacity is not information.
  if (!active) {
    parts.push(el("span", { class: "badge", text: t.t("order.status.expired") }));
  }

  // The summary is for participants (F1.3): the creator, anybody holding an
  // item, and the administrator. Offering it to somebody the summary page would
  // refuse is exactly what docs/06_ui_ux.md says not to do, and participation is
  // the one thing the list endpoint cannot tell us -- it carries no item data
  // for anybody (ADR-0011) -- so the detail fetched above decides it.
  const me = app.session.user?.id;
  const participant =
    detail !== null &&
    me !== undefined &&
    (app.session.isAdmin ||
      detail.creator_id === me ||
      detail.items.some((item) => item.user_id === me));

  const controls: Child[] = [];
  if (participant) {
    controls.push(
      el("a", {
        class: "button button-quiet",
        href: `/orders/${order.id}/summary`,
        text: t.t("order.summary"),
      }),
    );
  }

  // The creator's own controls, as icons: a grid of tiles has no room for three
  // words per tile, and a pencil and a bin are the two icons everybody already
  // knows. Each still carries a name for a screen reader and a tooltip for a
  // pointer. Editing is offered only while the order is open, because F6.6
  // makes an expired order read-only for everybody including its creator, and a
  // control that cannot be used is not shown at all.
  const mine = me !== undefined && order.creator_id === me;
  if (mine && active) {
    controls.push(
      iconLink(`/orders/${order.id}`, "pencil", t.t("order.edit")),
      iconButton("trash", t.t("order.delete"), "button-danger", () => {
        void removeOrder(app, order, detail, status);
      }),
    );
  }

  return tile(
    {
      href: `/orders/${order.id}`,
      title: order.title,
      faded: !active,
      media: logo(order, t),
      ...(controls.length > 0 ? { actions: tileActions(...controls) } : {}),
    },
    ...parts,
  );
}

/** A square icon control that leads somewhere. */
function iconLink(href: string, name: string, label: string): HTMLElement {
  return el("a", { class: "button button-icon", href, "aria-label": label, title: label }, icon(name));
}

/** A square icon control that does something. */
function iconButton(
  name: string,
  label: string,
  variant: string,
  onclick: () => void,
): HTMLElement {
  return el(
    "button",
    { type: "button", class: `button button-icon ${variant}`, "aria-label": label, title: label, onclick },
    icon(name),
  );
}

/**
 * Deleting an order from its tile.
 *
 * Asks first, and reloads rather than removing the tile by hand: the page is
 * cheap to rebuild and a tile spliced out of a grid that the server has since
 * reordered is a page that disagrees with itself.
 */
async function removeOrder(
  app: App,
  order: OrderListEntry,
  detail: OrderDetail | null,
  status: StatusLine,
): Promise<void> {
  const { t } = app;

  // The same warning the order page gives: deleting an order takes everybody
  // else's items with it, and the number of other people affected is the fact
  // that decides whether somebody goes through with it.
  const others = new Set(
    (detail?.items ?? [])
      .filter((item) => item.user_id !== app.session.user?.id)
      .map((item) => item.user_id),
  );

  const agreed = await confirmDialog({
    t,
    message: t.t("confirm.delete_order"),
    ...(others.size > 0
      ? { detail: t.t("confirm.delete_order.participants", { count: others.size }) }
      : {}),
    confirmLabel: t.t("action.delete"),
  });
  if (!agreed) {
    return;
  }

  try {
    await api.delete(`/orders/${order.id}`);
    app.router.refresh();
  } catch (error) {
    status.fail(errorMessage(t, error));
  }
}

/**
 * The restaurant's logo, or ours.
 *
 * A restaurant nobody has given a picture to used to get an empty dashed box,
 * which read as a missing image rather than as a restaurant. The doenerstag
 * mark says the same thing and looks deliberate.
 */
function logo(order: OrderHeader, t: App["t"]): HTMLElement {
  if (!order.restaurant_logo_image_id) {
    return el(
      "div",
      { class: "tile-logo tile-logo-fallback" },
      logoMark({ label: t.t("app.name") }),
    );
  }
  return el("img", {
    class: "tile-logo",
    src: thumbnailURL(order.restaurant_logo_image_id),
    alt: "",
    loading: "lazy",
  });
}
