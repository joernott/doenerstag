// What every part of the order page knows: the order as last read, the menu,
// and the questions they all ask about it.
//
// The page was one function of some 1,150 lines whose parts shared this as
// closure variables (20.29). They are modules now, and this is what they
// share. The order and the menu are properties rather than copies, because
// a refresh replaces them and every part must see the new ones.

import type { App } from "../../app";
import { api, getList } from "../../api";
import type { MoneyFormat } from "../../format";
import type { ReferenceData } from "../../reference";
import type { StatusLine } from "../../components/status";
import { runAction } from "../../components/status";
import type { Category, MenuItem, OrderDetail, OrderHeader, RestaurantDetail } from "../../types";
import { isActive, participates } from "../orders";

export interface OrderContext {
  readonly app: App;
  readonly id: string;
  readonly reference: ReferenceData | null;
  readonly restaurant: RestaurantDetail | null;
  readonly moneyFormat: MoneyFormat;
  /** The page's own status line, for failures that belong to no dialog. */
  readonly status: StatusLine;

  /** The order as last read: the header, or the detail when signed in. */
  order: OrderHeader | OrderDetail;
  menuItems: MenuItem[];
  categoryList: Category[];

  /** The detail, when this visitor is entitled to it. */
  readonly detail: () => OrderDetail | null;
  readonly active: () => boolean;
  /** Whether this visitor may still change their own items (F6.1, F6.6). */
  readonly canOrder: () => boolean;
  /** Whether this visitor may change the order itself (F5.7). */
  readonly isCreator: () => boolean;
  /**
   * Whether this visitor may read the summary (F1.3). The server decides for
   * real; this decides whether the link is worth offering.
   */
  readonly isParticipant: () => boolean;

  /** Reads the order again and redraws the page from it. */
  readonly refresh: () => Promise<void>;
  /** Reads the menu again and redraws the menu column. */
  readonly reloadMenu: () => Promise<void>;
}

export interface OrderContextOptions {
  app: App;
  id: string;
  order: OrderHeader | OrderDetail;
  reference: ReferenceData | null;
  restaurant: RestaurantDetail | null;
  moneyFormat: MoneyFormat;
  status: StatusLine;
  menuItems: MenuItem[];
  categoryList: Category[];
  /** Redraws everything that depends on the order. */
  render: () => void;
  /** Redraws the menu column. */
  renderMenu: () => void;
}

export function orderContext(options: OrderContextOptions): OrderContext {
  const { app, id, status } = options;
  const { t } = app;

  const ctx: OrderContext = {
    app,
    id,
    reference: options.reference,
    restaurant: options.restaurant,
    moneyFormat: options.moneyFormat,
    status,
    order: options.order,
    menuItems: options.menuItems,
    categoryList: options.categoryList,

    detail: () => ("items" in ctx.order ? ctx.order : null),
    active: () => isActive(ctx.order),
    canOrder: () => app.session.isAuthenticated && ctx.active(),
    isCreator: () => {
      const own = ctx.detail();
      return own !== null && app.session.user?.id === own.creator_id;
    },
    isParticipant: () => {
      const own = ctx.detail();
      return own !== null && participates(app, own);
    },

    refresh: async () => {
      await runAction({ status, t }, async () => {
        ctx.order = await api.get<OrderHeader | OrderDetail>(`/orders/${id}`);
        options.render();
      });
    },
    reloadMenu: async () => {
      [ctx.categoryList, ctx.menuItems] = await Promise.all([
        getList<Category>(`/restaurants/${ctx.order.restaurant_id}/categories`, "categories"),
        getList<MenuItem>(menuPath(ctx.order), "menu_items"),
      ]);
      options.renderMenu();
    },
  };
  return ctx;
}

/**
 * The menu as it will be when the food is fetched.
 *
 * `at` is the order's fulfilment time, not now: somebody ordering on Thursday
 * for Friday lunch is offered Friday's menu. The server answers with a verdict
 * per item rather than with the rules, so the question "is this served then"
 * has one implementation and it is the one that also refuses the order.
 */
export function menuPath(order: OrderHeader): string {
  const at = encodeURIComponent(order.fulfilment_at);
  return `/restaurants/${order.restaurant_id}/menu-items?at=${at}`;
}
