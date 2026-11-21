// The order overview: the start page.
//
// A grid of tiles whose first entry opens a new order. Active orders first,
// then expired ones faded but fully clickable -- and the server has already put
// them in that order, so this page does not sort again.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { currentPath, loginHref } from "../returnto";
import { el, icon, type Child } from "../dom";
import { formatDateTime, formatRelativeTime, fulfilmentLabel, orderTitle } from "../format";
import { logoMark } from "../logo";
import { button, iconButton } from "../components/forms";
import { thumbnailURL } from "../components/images";
import { confirmDialog, openModal } from "../components/modal";
import { addTile, tile, tileActions, tileGrid } from "../components/tiles";
import { failedPage, overviewPage, overviewPageWithActions } from "./page";
import type { OrderDetail, OrderHeader, OrderListEntry, Vote } from "../types";
import { voteOpen, voteTitle } from "./vote";
import { runAction, statusLine, type StatusLine } from "../components/status";

/** Whether an order is still open, judged on the viewer's clock. */
export function isActive(order: OrderHeader, now: Date = new Date()): boolean {
  return new Date(order.deadline_at).getTime() > now.getTime();
}

export async function ordersPage(app: App): Promise<HTMLElement> {
  const { t } = app;

  let orders: OrderListEntry[];
  let votes: Vote[];
  try {
    [orders, votes] = await Promise.all([
      getList<OrderListEntry>("/orders", "orders"),
      // The open votes sit among the open orders (F12.13). A failure here
      // leaves the orders, which are the page's main business.
      getList<Vote>("/votes", "votes").catch(() => []),
    ]);
  } catch (error) {
    return failedPage(t.t("nav.orders"), t, error);
  }

  // Whether this visitor takes part in each order, which decides whether the
  // tile offers its summary. The list carries no item data for anybody
  // (ADR-0011), so it cannot answer that, and the order itself is fetched per
  // entry and in parallel to find out. Affordable at the scale ADR-0006
  // assumes; the tiles do not show a participant count or a total, so this is
  // the only thing the extra request buys.
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

  const create = app.session.isAuthenticated ? "/orders/new" : loginHref(currentPath());
  const orderTiles = orders.map((order, index) => ({
    at: new Date(order.fulfilment_at).getTime(),
    active: isActive(order),
    element: orderTile(app, order, details[index] ?? null, status),
  }));

  // Votes go among the active orders by fulfilment time, as docs/06 sorts
  // the orders themselves; the expired orders stay at the end.
  const tiles = orderTiles.filter((entry) => entry.active);
  for (const vote of votes) {
    const at = new Date(vote.fulfilment_at).getTime();
    const index = tiles.findIndex((entry) => entry.at > at);
    const entry = { at, active: true, element: voteTile(app, vote, status) };
    if (index < 0) {
      tiles.push(entry);
    } else {
      tiles.splice(index, 0, entry);
    }
  }
  tiles.push(...orderTiles.filter((entry) => !entry.active));

  const plus = addTile(create, t.t("order.new"));
  if (app.session.isAuthenticated) {
    // The plus asks what to start: an order, or a vote on where to order
    // (F12). An anonymous visitor's plus still leads to the login page.
    plus.querySelector("a")?.addEventListener("click", (event) => {
      event.preventDefault();
      chooseWhatToStart(app);
    });
  }

  const body = [status.element, tileGrid(plus, ...tiles.map((entry) => entry.element))];

  // Only the administrator is offered the cleanup. The endpoint refuses
  // everybody else anyway; hiding the button keeps the page from advertising
  // something most people cannot do.
  if (!app.session.isAdmin) {
    return overviewPage(t.t("nav.orders"), ...body);
  }
  return overviewPageWithActions(
    t.t("nav.orders"),
    cleanupButton(app, status, () => app.router.refresh()),
    ...body,
  );
}

/**
 * One order tile.
 *
 * What it shows is deliberately thin: the restaurant, when the food arrives and
 * when the order closes. The item count, the participant count, the running
 * total and who opened it are left to the order itself, which is one click
 * away: a grid of tiles is a place to choose from rather than a place to read
 * from.
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
      text: `${fulfilmentLabel(t, order.fulfilment)} · ${formatDateTime(app.language, order.fulfilment_at)}`,
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

  // The summary is for participants (F1.3). Offering it to somebody the summary
  // page would refuse is exactly what docs/06_ui_ux.md says not to do, and
  // participation is the one thing the list endpoint cannot tell us -- it
  // carries no item data for anybody (ADR-0011) -- so the detail fetched above
  // decides it.
  const me = app.session.user?.id;
  const participant = detail !== null && participates(app, detail);

  const controls: Child[] = [summaryLink(app, order.id, participant)];

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
      title: orderTitle(app.language, order),
      faded: !active,
      media: logo(order, t),
      ...(controls.length > 0 ? { actions: tileActions(...controls) } : {}),
    },
    ...parts,
  );
}

/**
 * The dialog behind the plus tile: an order, or a vote first (docs/06).
 */
function chooseWhatToStart(app: App): void {
  const { t } = app;
  const option = (href: string, label: string, hint: string): HTMLElement =>
    el(
      "a",
      {
        class: "start-option",
        href,
        onclick: (event: Event) => {
          event.preventDefault();
          modal.close();
          app.navigate(href);
        },
      },
      el("span", { class: "start-option-label", text: label }),
      el("span", { class: "muted", text: hint }),
    );
  const modal = openModal({
    title: t.t("start.title"),
    closeLabel: t.t("action.close"),
    body: el(
      "div",
      { class: "start-options" },
      option("/orders/new", t.t("start.order"), t.t("start.order.hint")),
      option("/votes/new", t.t("start.vote"), t.t("start.vote.hint")),
    ),
  });
}

/**
 * One vote tile (docs/06): marked as a vote, with its decision time and the
 * restaurant ahead. Its logo is the leader's, or the mark while nobody has
 * voted. No summary button: a vote has none.
 */
function voteTile(app: App, vote: Vote, status: StatusLine): HTMLElement {
  const { t } = app;
  const leader = vote.choices.find((choice) => choice.id === vote.leader_choice_id) ?? null;
  const parts: Child[] = [
    el("span", { class: "badge", text: t.t("vote.badge") }),
    el(
      "span",
      { class: "muted" },
      `${t.t("vote.decision.label")}: ${formatDateTime(app.language, vote.decision_at)}`,
      voteOpen(vote)
        ? el("span", { class: "tile-hint", text: ` (${formatRelativeTime(app.language, vote.decision_at)})` })
        : null,
    ),
    el("span", {
      class: "muted",
      text: leader
        ? t.t("vote.tile.leader", { name: leader.restaurant_name, count: leader.count, total: vote.total })
        : t.t("vote.no_votes"),
    }),
  ];

  const controls: Child[] = [];
  const me = app.session.user?.id;
  if (me !== undefined && vote.creator_id === me && voteOpen(vote)) {
    controls.push(
      iconLink(`/votes/${vote.id}`, "pencil", t.t("vote.edit")),
      iconButton("trash", t.t("vote.delete"), "button-danger", () => {
        void removeVote(app, vote, status);
      }),
    );
  }

  const media = leader?.restaurant_logo_image_id
    ? el("img", { class: "tile-logo", src: thumbnailURL(leader.restaurant_logo_image_id), alt: "", loading: "lazy" })
    : el("div", { class: "tile-logo tile-logo-fallback" }, logoMark({ label: t.t("app.name") }));

  return tile(
    {
      href: `/votes/${vote.id}`,
      title: voteTitle(app, vote),
      media,
      ...(controls.length > 0 ? { actions: tileActions(...controls) } : {}),
    },
    ...parts,
  );
}

async function removeVote(app: App, vote: Vote, status: StatusLine): Promise<void> {
  const { t } = app;
  const agreed = await confirmDialog({
    t,
    message: t.t("vote.confirm_delete"),
    ...(vote.total > 0 ? { detail: t.t("vote.confirm_delete.voters", { count: vote.total }) } : {}),
    confirmLabel: t.t("action.delete"),
  });
  if (!agreed) {
    return;
  }
  try {
    await api.delete(`/votes/${vote.id}`);
    app.router.refresh();
  } catch (error) {
    status.fail(errorMessage(t, error));
  }
}

/**
 * Whether this visitor is one of the people the summary is for (F1.3).
 *
 * The creator, the person collecting the money, the person fetching the food,
 * anybody holding an item, and the administrator -- the same rule as
 * db.IsParticipant, in one place so the overview and the order page cannot
 * come to offer the summary to different people.
 */
export function participates(app: App, order: OrderDetail): boolean {
  const me = app.session.user?.id;
  if (me === undefined) {
    return false;
  }
  return (
    app.session.isAdmin ||
    order.creator_id === me ||
    order.money_collector_id === me ||
    order.pickup_person_id === me ||
    order.items.some((item) => item.user_id === me)
  );
}

/**
 * The link to an order's summary.
 *
 * Always rendered, and disabled with the reason when this visitor is not one of
 * the people the summary is for (F1.3). Leaving it out entirely would leave
 * somebody wondering whether the feature exists at all; a control that is
 * visibly unavailable and says why answers that.
 *
 * The disabled form is a button rather than a link, because a link that goes
 * nowhere is not a link. The explanation is error 3004's own text, so the
 * tooltip and the refusal the server would send say the same thing.
 */
export function summaryLink(app: App, orderID: string, allowed: boolean): HTMLElement {
  const { t } = app;
  if (allowed) {
    return el("a", {
      class: "button button-primary",
      href: `/orders/${orderID}/summary`,
      text: t.t("order.summary"),
    });
  }
  return el("button", {
    type: "button",
    class: "button button-primary",
    text: t.t("order.summary"),
    disabled: true,
    title: t.t("error.3004"),
  });
}

/** A square icon control that leads somewhere. */
function iconLink(href: string, name: string, label: string): HTMLElement {
  return el("a", { class: "button button-icon", href, "aria-label": label, title: label }, icon(name));
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
 * A restaurant nobody has given a picture to gets the doenerstag mark. An empty
 * dashed box would read as a missing image rather than as a restaurant; the
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
/**
 * The Cleanup button, for the administrator only.
 *
 * It runs the same retention pass the nightly cron job runs, from the page
 * where the reason to want it is visible: a list with a dozen expired orders on
 * it. Nobody else sees the button, and the server refuses the endpoint for
 * anybody else regardless -- the hidden button is a convenience, not the
 * control.
 *
 * It says what it is about to do before doing it. "Cleanup" is a mild word for
 * deleting orders, and the confirmation names the retention period so that the
 * person pressing it knows what counts as old.
 */
function cleanupButton(app: App, status: StatusLine, reload: () => void): HTMLElement {
  const { t } = app;

  const control = button({
    label: t.t("admin.cleanup"),
    onclick: () => run(),
  });

  async function run(): Promise<void> {
    const confirmed = await confirmDialog({
      t,
      title: t.t("admin.cleanup"),
      message: t.t("admin.cleanup_confirm"),
      confirmLabel: t.t("admin.cleanup"),
    });
    if (!confirmed) {
      return;
    }

    await runAction({ status, t, busy: control }, async () => {
      const report = await api.post<{ total: number }>("/cleanup", {});
      status.say(
        report.total > 0
          ? t.t("admin.cleanup_done", { count: report.total })
          : t.t("admin.cleanup_nothing"),
      );
      reload();
    });
  }

  return control;
}
