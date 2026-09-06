// The route table, and the pages that exist so far.
//
// Sprint 10 builds the shell, not the screens: every route resolves, the
// chrome renders around it and the URL works, but most pages say plainly that
// they are not built yet rather than pretending. Sprints 11 and 12 replace them
// one at a time, and the route table is where that happens.

import type { App } from "../app";
import { api, type VersionInfo } from "../api";
import { el } from "../dom";
import { formatDateTime } from "../format";
import type { Route } from "../router";

/** The routes, in the order the router tries them. */
export function routes(app: App): Route[] {
  return [
    { pattern: "/", render: () => placeholder(app, app.t.t("nav.orders")) },
    { pattern: "/orders/:id", render: () => placeholder(app, app.t.t("nav.orders")) },
    { pattern: "/orders/:id/summary", render: () => placeholder(app, app.t.t("order.summary")) },
    { pattern: "/restaurants", render: () => placeholder(app, app.t.t("nav.restaurants")) },
    { pattern: "/restaurants/:id", render: () => placeholder(app, app.t.t("nav.restaurants")) },
    { pattern: "/account", render: () => placeholder(app, app.t.t("nav.account")) },
    { pattern: "/admin/users", render: () => placeholder(app, app.t.t("nav.users")) },
    { pattern: "/version", render: () => versionPage(app) },
    { pattern: "/imprint", render: () => placeholder(app, app.t.t("nav.imprint")) },
    { pattern: "/legal-notes", render: () => placeholder(app, app.t.t("nav.legal_notes")) },
  ];
}

/** A page with a heading and a body, which every page here is. */
export function page(title: string, ...content: (Node | string | null)[]): HTMLElement {
  const article = el("article", { class: "page-body" }, el("h1", { class: "page-title", text: title }));
  for (const item of content) {
    if (item !== null) {
      article.appendChild(typeof item === "string" ? document.createTextNode(item) : item);
    }
  }
  document.title = `${title} — doenerstag`;
  return article;
}

/** A screen a later sprint fills in. */
function placeholder(app: App, title: string): HTMLElement {
  return page(title, el("p", { class: "muted", text: app.t.t("state.not_yet") }));
}

/**
 * The version page.
 *
 * Real rather than a placeholder, because it is the one screen whose API
 * exists in full -- and because it exercises the whole foundation at once: a
 * request through the client, a translated label and a date formatted in the
 * interface locale.
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
