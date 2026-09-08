// The summary page: what somebody reads down the telephone.
//
// Four sections (docs/06_ui_ux.md): the header with the restaurant's number as
// a tel: link, the aggregated list, who owes what, and the totals. The
// aggregated list is the one a person actually dictates, so it is the visually
// dominant one and is set larger than everything else on the page.

import type { App } from "../app";
import { api, ApiError, errorMessage } from "../api";
import { append, el, type Child } from "../dom";
import { formatDateTime, formatMoney, formatRelativeTime } from "../format";
import { button } from "../components/forms";
import { minorUnitOf, referenceData } from "../reference";
import { isActive, type OrderHeader } from "./orders";
import { actions, page, section, statusLine } from "./page";
import type { Contact, Restaurant } from "./restaurant";

interface AggregatedLine {
  item_name: string;
  external_id: string;
  modifications: string[];
  note: string | null;
  count: number;
  unit_price_cents: number;
  total_cents: number;
}

interface PersonLine {
  user_id: string;
  display_name: string;
  items: {
    id: string;
    quantity: number;
    item_name: string;
    note: string;
    modifications: { name: string }[];
    line_total_cents: number;
  }[];
  total_cents: number;
}

interface Summary {
  order_id: string;
  title: string;
  currency_code: string;
  aggregated: AggregatedLine[];
  per_person: PersonLine[];
  item_total_cents: number;
  delivery_fee_cents: number | null;
  grand_total_cents: number;
  min_order_value_cents: number | null;
  below_minimum: boolean;
  plain_text: string;
}

interface RestaurantDetail extends Restaurant {
  contacts: Contact[];
}

export async function summaryPage(app: App, id: string): Promise<HTMLElement> {
  let summary: Summary;
  try {
    summary = await api.get<Summary>(`/orders/${id}/summary`);
  } catch (error) {
    return refused(app, id, error);
  }

  // The header wants the restaurant's telephone number, the fulfilment time and
  // the deadline, none of which the summary carries: it is about the
  // aggregation. Both are public reads, and a failure of either leaves a page
  // that is still worth reading.
  const order = await api.get<OrderHeader>(`/orders/${id}`).catch(() => null);
  const restaurant = order
    ? await api.get<RestaurantDetail>(`/restaurants/${order.restaurant_id}`).catch(() => null)
    : null;
  const currencies = await referenceData()
    .then((reference) => reference.currencies)
    .catch(() => []);
  const minorUnit = minorUnitOf(currencies, summary.currency_code);

  const money = (cents: number): string =>
    formatMoney(app.language, cents, summary.currency_code, minorUnit);

  return page(
    summary.title,
    headerCard(app, summary, order, restaurant),
    aggregatedCard(app, summary, money),
    perPersonCard(app, summary, money),
    totalsCard(app, summary, money),
    toolbar(app, summary),
  );
}

/**
 * The page a non-participant gets.
 *
 * An explanation rather than a raw 403 (docs/06_ui_ux.md): a bookmark, a pasted
 * link or an order somebody has since left all end here, and none of them is a
 * mistake worth shouting about. Somebody who is not logged in gets a login
 * link, because logging in may well make them a participant.
 */
function refused(app: App, id: string, error: unknown): HTMLElement {
  const { t } = app;

  if (error instanceof ApiError && (error.code === 3004 || error.isAuthentication)) {
    return page(
      t.t("order.summary"),
      section(
        t.t("order.summary"),
        el("p", { text: t.t("summary.participants_only") }),
        app.session.isAuthenticated
          ? null
          : el("p", {}, el("a", { class: "link", href: "/account", text: t.t("order.join") })),
        el("p", {}, el("a", { class: "link", href: `/orders/${id}`, text: t.t("summary.back") })),
      ),
    );
  }

  return page(
    t.t("order.summary"),
    el("p", { class: "field-error", text: errorMessage(t, error) }),
  );
}

function headerCard(
  app: App,
  summary: Summary,
  order: OrderHeader | null,
  restaurant: RestaurantDetail | null,
): HTMLElement {
  const { t } = app;
  const rows: [string, Child][] = [];

  if (order) {
    rows.push([
      t.t("order.fulfilment.type"),
      t.t(order.fulfilment === "delivery" ? "order.fulfilment.delivery" : "order.fulfilment.pickup"),
    ]);
    rows.push([t.t("order.fulfilment.label"), formatDateTime(app.language, order.fulfilment_at)]);
    rows.push([
      t.t("order.deadline.label"),
      `${formatDateTime(app.language, order.deadline_at)} · ${
        isActive(order) ? t.t("order.status.active") : t.t("order.status.expired")
      } (${formatRelativeTime(app.language, order.deadline_at)})`,
    ]);
  }

  // The telephone number is the point of this section: it is what the person
  // holding the phone needs, so it is a link and it is large.
  const phone = (restaurant?.contacts ?? []).find((contact) => contact.render_as === "tel");
  const contactRow: Child = phone
    ? el("a", { class: "phone", href: `tel:${phone.value}`, text: phone.value })
    : el("span", { class: "muted", text: t.t("restaurant.no_contact") });

  rows.unshift([t.t("restaurant.data"), restaurant?.name ?? order?.restaurant_name ?? ""]);
  rows.push([t.t("contact_type.phone"), contactRow]);

  const list = el("dl", { class: "definitions" });
  for (const [label, value] of rows) {
    list.appendChild(el("dt", { text: label }));
    const dd = el("dd");
    append(dd, value);
    list.appendChild(dd);
  }

  return section(summary.title, list);
}

/**
 * What to order: the section that is read out.
 *
 * One row per distinct combination of dish, options and note, which is what the
 * API aggregates. The item number is included where the menu still has one --
 * "number 12, three times" is how this conversation actually goes.
 */
function aggregatedCard(
  app: App,
  summary: Summary,
  money: (cents: number) => string,
): HTMLElement {
  const { t } = app;

  if (summary.aggregated.length === 0) {
    return section(t.t("summary.what_to_order"), el("p", { class: "muted", text: t.t("order.empty") }));
  }

  const rows = summary.aggregated.map((line) => {
    const details: Child[] = [];
    if (line.modifications.length > 0) {
      details.push(el("span", { class: "muted", text: line.modifications.join(", ") }));
    }
    if (line.note) {
      details.push(el("span", { class: "muted item-note", text: line.note }));
    }

    return el(
      "li",
      { class: "dictation-line" },
      el("span", { class: "dictation-count", text: `${String(line.count)}×` }),
      el(
        "span",
        { class: "dictation-item" },
        line.external_id ? el("span", { class: "menu-number", text: line.external_id }) : null,
        el("span", { text: line.item_name }),
        details.length > 0 ? el("span", { class: "dictation-details" }, ...details) : null,
      ),
      el("span", { class: "dictation-total", text: money(line.total_cents) }),
    );
  });

  return section(
    t.t("summary.what_to_order"),
    el("ul", { class: "plain-list dictation" }, ...rows),
  );
}

/** Who owes what: one block per person, with their own total. */
function perPersonCard(
  app: App,
  summary: Summary,
  money: (cents: number) => string,
): HTMLElement {
  const { t } = app;

  const blocks = summary.per_person.map((person) =>
    el(
      "div",
      { class: "person-group" },
      el(
        "h3",
        { class: "person-name" },
        // A deleted account is shown as the placeholder's display name, which
        // is what the API sends; the page does not invent a label for it.
        person.display_name,
        el("span", { class: "person-total", text: money(person.total_cents) }),
      ),
      el(
        "ul",
        { class: "plain-list" },
        ...person.items.map((item) =>
          el(
            "li",
            { class: "order-item" },
            el("span", { class: "item-quantity", text: `${String(item.quantity)}×` }),
            el(
              "span",
              { class: "order-item-text" },
              el("span", { text: item.item_name }),
              item.modifications.length > 0
                ? el("span", {
                    class: "muted",
                    text: ` ${item.modifications.map((entry) => entry.name).join(", ")}`,
                  })
                : null,
              item.note ? el("span", { class: "muted item-note", text: ` ${item.note}` }) : null,
            ),
            el("span", { class: "menu-price", text: money(item.line_total_cents) }),
          ),
        ),
      ),
    ),
  );

  return section(t.t("summary.who_owes_what"), ...blocks);
}

function totalsCard(
  app: App,
  summary: Summary,
  money: (cents: number) => string,
): HTMLElement {
  const { t } = app;
  const list = el("dl", { class: "definitions totals" });

  const add = (label: string, value: string, className?: string): void => {
    list.appendChild(el("dt", { text: label, ...(className ? { class: className } : {}) }));
    list.appendChild(el("dd", { text: value, ...(className ? { class: className } : {}) }));
  };

  add(t.t("order.total"), money(summary.item_total_cents));
  if (summary.delivery_fee_cents) {
    add(t.t("order.delivery_fee"), money(summary.delivery_fee_cents));
  }
  add(t.t("order.total"), money(summary.grand_total_cents), "grand");

  return section(
    t.t("order.totals"),
    list,
    summary.below_minimum
      ? el("p", {
          class: "notice notice-warning",
          role: "status",
          text:
            summary.min_order_value_cents === null
              ? t.t("order.below_minimum")
              : `${t.t("order.below_minimum")} (${money(summary.min_order_value_cents)})`,
        })
      : null,
  );
}

/**
 * Copy and print.
 *
 * The copied text is the API's own rendering, not a second one built here: the
 * server already writes the wording, and a page that assembled its own would
 * eventually disagree with a script that used the endpoint.
 */
function toolbar(app: App, summary: Summary): HTMLElement {
  const { t } = app;
  const status = statusLine();

  const offerTheText = (): void => {
    // No clipboard permission, or no clipboard at all. Showing the text is the
    // fallback: it can still be selected and copied by hand.
    status.fail(t.t("summary.copy_failed"));
    fallback.hidden = false;
    fallback.select();
  };

  const copy = button({
    label: t.t("summary.copy"),
    onclick: () => {
      // The clipboard API is absent outside a secure context -- and a
      // development server with a self-signed certificate is one such place, so
      // this is not a hypothetical branch. Without the check the button did
      // nothing at all: no copy, no message, no fallback.
      if (!navigator.clipboard) {
        offerTheText();
        return;
      }

      void navigator.clipboard
        .writeText(summary.plain_text)
        .then(() => {
          status.say(t.t("action.copied"));
        })
        .catch(offerTheText);
    },
  });

  const fallback = el("textarea", {
    class: "input",
    rows: 12,
    readonly: true,
    hidden: true,
    "aria-label": t.t("summary.plain_text"),
  });
  fallback.value = summary.plain_text;

  const print = button({
    label: t.t("summary.print"),
    onclick: () => {
      window.print();
    },
  });

  return section(t.t("summary.actions"), actions(copy, print), fallback, status.element);
}
