// The route table, and the pages that exist so far.
//
// Sprint 11 filled in the account, the restaurants and the menus. Orders are
// sprint 12, and their routes still resolve to a page that says so rather than
// to a 404: the URL is right, the screen is not built.

import type { App } from "../app";
import { api, type VersionInfo } from "../api";
import { el } from "../dom";
import { formatDateTime } from "../format";
import type { Route } from "../router";
import { accountPage } from "./account";
import { createOrderPage } from "./ordercreate";
import { orderPage } from "./order";
import { ordersPage } from "./orders";
import { page } from "./page";
import { restaurantPage } from "./restaurant";
import { restaurantsPage } from "./restaurants";

export { page } from "./page";

/** The routes, in the order the router tries them. */
export function routes(app: App): Route[] {
  return [
    { pattern: "/", render: () => ordersPage(app) },
    { pattern: "/orders/new", render: () => createOrderPage(app) },
    {
      pattern: "/orders/:id",
      render: (context) => orderPage(app, context.params["id"] ?? "", { cleanup: context }),
    },
    { pattern: "/orders/:id/summary", render: () => placeholder(app, app.t.t("order.summary")) },
    { pattern: "/restaurants", render: () => restaurantsPage(app) },
    { pattern: "/restaurants/:id", render: (context) => restaurantPage(app, context.params["id"] ?? "") },
    { pattern: "/account", render: () => accountPage(app) },
    { pattern: "/admin/users", render: () => placeholder(app, app.t.t("nav.users")) },
    { pattern: "/version", render: () => versionPage(app) },
    { pattern: "/imprint", render: () => placeholder(app, app.t.t("nav.imprint")) },
    { pattern: "/legal-notes", render: () => placeholder(app, app.t.t("nav.legal_notes")) },
  ];
}

/** A screen a later sprint fills in. */
function placeholder(app: App, title: string): HTMLElement {
  return page(title, el("p", { class: "muted", text: app.t.t("state.not_yet") }));
}

/**
 * The version page.
 *
 * It exercises the whole foundation at once: a request through the client, a
 * translated label and a date formatted in the interface locale.
 */
async function versionPage(app: App): Promise<HTMLElement> {
  const { t } = app;
  let info: VersionInfo | null = app.version;
  if (!info) {
    try {
      info = await api.get<VersionInfo>("/version");
      app.version = info;
    } catch {
      info = null;
    }
  }

  if (!info) {
    return page(t.t("nav.version"), el("p", { class: "muted", text: t.t("state.offline") }));
  }

  const rows: [string, string][] = [
    [t.t("version.application"), info.version],
    [t.t("version.commit"), info.commit],
    [t.t("version.built"), formatDateTime(app.language, info.build_date)],
  ];

  const list = el("dl", { class: "definitions" });
  for (const [label, value] of rows) {
    list.appendChild(el("dt", { text: label }));
    list.appendChild(el("dd", { text: value }));
  }

  return page(t.t("nav.version"), list);
}

/**
 * The page for a URL that matches nothing.
 *
 * The server cannot tell a mistyped path from a valid one -- it serves the
 * shell for both -- so this is where a wrong URL is finally answered, and it
 * answers with the same 4000 wording the API uses.
 */
export function notFoundPage(app: App): HTMLElement {
  const { t } = app;
  return page(
    t.t("error.title"),
    el("p", { text: t.t("error.4000") }),
    el("p", {}, el("a", { class: "link", href: "/", text: t.t("nav.home") })),
  );
}
