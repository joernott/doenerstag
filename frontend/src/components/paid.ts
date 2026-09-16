// The paid tick beside a price, shared by the order page and the summary.
//
// Not a payment: the application handles none. It is the person holding the
// money marking a line off a list, which they were doing on paper. The server
// decides who may make the tick and keeps it out of the deadline rule (see
// setItemPaid); this module decides only whether to offer a live control or a
// read-only one, and it is one module so that the two pages cannot come to
// offer it to different people.

import type { App } from "../app";
import { api } from "../api";
import { el } from "../dom";

/** The people an order has, as far as ticking a line is concerned. */
export interface PaidRights {
  /** Whoever added the line. */
  ownerID: string;
  /** Whoever opened the order, when the page knows it. */
  creatorID?: string | null;
  /** Whoever is collecting the money, when anybody is. */
  moneyCollectorID?: string | null;
}

/**
 * Whether this visitor may tick a line as settled.
 *
 * The three people who plausibly know whether the money changed hands --
 * whoever ordered it, whoever opened the order, and whoever is collecting --
 * and the administrator. Not gated on the deadline: the money changes hands
 * when the food arrives, which is always after it.
 */
export function mayTickPaid(app: App, rights: PaidRights): boolean {
  const me = app.session.user?.id;
  if (me === undefined) {
    return false;
  }
  return (
    app.session.isAdmin ||
    rights.ownerID === me ||
    (rights.creatorID ?? null) === me ||
    (rights.moneyCollectorID ?? null) === me
  );
}

export interface PaidCheckboxOptions {
  app: App;
  orderID: string;
  itemID: string;
  itemName: string;
  paid: boolean;
  /** Whether this visitor may change it; otherwise it is shown and disabled. */
  allowed: boolean;
  /**
   * Called once the server has agreed. The page reloads what it shows from the
   * server rather than adjusting its own sums, because the sums are the API's:
   * there is no second copy of that arithmetic here to fall out of step.
   */
  onChanged: () => void | Promise<void>;
}

/**
 * The tick itself.
 *
 * Shown to everybody who can see the line, because whether it has been settled
 * is part of what the page says; live only for those who may change it. Its
 * accessible name names the dish rather than saying "paid", because a list of
 * nine checkboxes all called "paid" cannot be navigated by ear.
 */
export function paidCheckbox(options: PaidCheckboxOptions): HTMLElement {
  const { app, orderID, itemID, itemName, paid, allowed, onChanged } = options;
  const { t } = app;
  const label = t.t("item.paid_for", { item: itemName });

  const box = el("input", {
    type: "checkbox",
    class: "checkbox",
    checked: paid,
    disabled: !allowed,
    "aria-label": label,
    title: label,
  });

  if (allowed) {
    box.addEventListener("change", () => {
      const wanted = box.checked;
      box.disabled = true;
      api
        .put(`/orders/${orderID}/items/${itemID}/paid`, { paid: wanted })
        .then(onChanged)
        .catch(() => {
          // Put the box back: the server did not agree, and a tick that exists
          // only in this browser is worse than none.
          box.checked = !wanted;
          box.disabled = false;
        });
    });
  }

  // A visible label, not only an accessible name. The tick means nothing on
  // its own -- a column of bare boxes beside prices could be anything -- and
  // "Paid" is what a person reading the page, or the printed summary with a
  // pen in hand, needs to see. The input keeps its own name naming the dish,
  // which contains the visible word, so the two agree (WCAG 2.5.3).
  return el("label", { class: "paid-box" }, box, el("span", { class: "paid-label", text: t.t("item.paid") }));
}
