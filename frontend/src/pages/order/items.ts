// What has been ordered: the items grouped by person, the totals, and the
// dialog that adds or changes an item. Part of the order page (20.29).

import { api, errorMessage } from "../../api";
import { currentPath, loginHref } from "../../returnto";
import { el, keyed, type Child } from "../../dom";
import { definitionList } from "../../components/definitions";
import { mayTickPaid, paidCheckbox } from "../../components/paid";
import { formatMoney, moneyInputValue } from "../../format";
import {
  checkbox,
  field,
  iconButton,
  input,
  textarea,
} from "../../components/forms";
import { openFormDialog } from "../../components/formdialog";
import { confirmDialog } from "../../components/modal";
import type { MenuItem, OrderDetail, OrderItem } from "../../types";
import { actions, section } from "../page";
import type { OrderContext } from "./context";

export interface OrderItems {
  itemsCard: (own: OrderDetail) => HTMLElement;
  totalsCard: (own: OrderDetail) => HTMLElement;
  /** What an anonymous visitor sees instead of the items: how many there are. */
  anonymousCard: () => HTMLElement;
  /** Opens the dialog for a new item (from `menuItem`) or an existing one. */
  openItemEditor: ItemEditor;
}

/** Opens the item dialog; the menu column calls it to order a dish. */
export type ItemEditor = (
  existing: OrderItem | null,
  menuItem?: MenuItem,
) => Promise<void>;

/** What has been ordered, and the dialog that changes it. */
export function orderItems(ctx: OrderContext): OrderItems {
  const { app, id, moneyFormat, status, detail, canOrder, refresh } = ctx;
  const { t } = app;

  /** The items, grouped by the person who ordered them. */
  function itemsCard(own: OrderDetail): HTMLElement {
    if (own.items.length === 0) {
      return section(
        t.t("order.item_list"),
        el("p", { class: "muted", text: t.t("order.empty") }),
      );
    }

    const byPerson = new Map<string, OrderItem[]>();
    for (const item of own.items) {
      const group = byPerson.get(item.user_id) ?? [];
      group.push(item);
      byPerson.set(item.user_id, group);
    }

    const groups = [...byPerson].map(([userID, items]) => {
      const rows = items.map((item) => itemRow(item, userID));
      const personTotal = items.reduce(
        (sum, item) => sum + item.line_total_cents,
        0,
      );

      return el(
        "div",
        { class: "person-group" },
        el(
          "h3",
          { class: "person-name" },
          items[0]?.user_name ?? "",
          el("span", {
            class: "person-total",
            text: formatMoney(
              app.language,
              personTotal,
              ctx.order.currency_code,
              moneyFormat,
            ),
          }),
        ),
        el("ul", { class: "plain-list" }, ...rows),
      );
    });

    return section(t.t("order.item_list"), ...groups);
  }

  function itemRow(item: OrderItem, userID: string): HTMLElement {
    const mine = app.session.user?.id === userID;
    const details: Child[] = [
      el("span", { class: "item-quantity", text: `${item.quantity}×` }),
      el("strong", { text: item.item_name }),
    ];

    const extras: Child[] = [];
    if (item.modifications.length > 0) {
      extras.push(
        el("span", {
          class: "muted",
          text: item.modifications
            .map((modification) => modification.name)
            .join(", "),
        }),
      );
    }
    if (item.note) {
      extras.push(el("span", { class: "muted item-note", text: item.note }));
    }

    return el(
      "li",
      { class: "order-item" },
      el(
        "div",
        { class: "order-item-text" },
        el("div", { class: "order-item-head" }, ...details),
        extras.length > 0
          ? el("div", { class: "order-item-extras" }, ...extras)
          : null,
      ),
      // Own items only, and only while the order is open: F6.5 and F6.6.
      //
      // Before the tick and the price rather than after them. A row with these
      // two buttons and a row without them would otherwise end at different
      // places, and the amounts would not line up under each other or under
      // the person's total -- which is what the right-hand edge is for.
      //
      // A pencil and a bin rather than two words, as on the order overview's
      // tiles and for the same reason: with the paid tick and its label beside
      // the price, two text buttons squeezed the dish name onto two lines and
      // stacked on top of each other. Each names the dish, because a list has
      // one of these per line and "Edit" alone does not say which.
      mine && canOrder()
        ? actions(
            keyed(
              iconButton(
                "pencil",
                `${t.t("action.edit")}: ${item.item_name}`,
                "",
                () => {
                  void openItemEditor(item);
                },
              ),
              `edit-line:${item.id}`,
            ),
            keyed(
              iconButton(
                "trash",
                `${t.t("action.delete")}: ${item.item_name}`,
                "button-danger",
                () => {
                  void removeItem(item);
                },
              ),
              `delete-line:${item.id}`,
            ),
          )
        : null,
      // The paid tick, as on the summary. Not gated on the order being open
      // like the two buttons before it: the money changes hands when the food
      // arrives, which is always after the deadline.
      paidCheckbox({
        app,
        orderID: id,
        itemID: item.id,
        itemName: item.item_name,
        paid: item.paid,
        allowed: mayTickPaid(app, {
          ownerID: userID,
          creatorID: detail()?.creator_id,
          moneyCollectorID: detail()?.money_collector_id,
        }),
        // The whole order is read again, so the Totals box and every other
        // browser watching it follow from one answer.
        onChanged: refresh,
      }),
      el("span", {
        class: "menu-price",
        text: formatMoney(
          app.language,
          item.line_total_cents,
          ctx.order.currency_code,
          moneyFormat,
        ),
      }),
    );
  }

  function totalsCard(own: OrderDetail): HTMLElement {
    const money = (cents: number): string =>
      formatMoney(app.language, cents, ctx.order.currency_code, moneyFormat);

    // What is still owed and what has been ticked as settled, both from the
    // API, which sums them from the same items it sends. The delivery fee is
    // neither: nobody ticks it.
    const rows: [string, string][] = [
      [t.t("order.total_unpaid"), money(own.unpaid_total_cents)],
      [t.t("order.total_paid"), money(own.paid_total_cents)],
    ];
    if (ctx.order.delivery_fee_cents) {
      rows.push([
        t.t("order.delivery_fee"),
        formatMoney(
          app.language,
          ctx.order.delivery_fee_cents,
          ctx.order.currency_code,
          moneyFormat,
        ),
      ]);
    }

    const list = definitionList(rows, "definitions totals");
    list.appendChild(el("dt", { class: "grand", text: t.t("order.total") }));
    list.appendChild(
      el("dd", {
        class: "grand",
        text: formatMoney(
          app.language,
          own.grand_total_cents,
          ctx.order.currency_code,
          moneyFormat,
        ),
      }),
    );

    return section(
      t.t("order.totals"),
      list,
      own.below_minimum
        ? el("p", {
            class: "notice notice-warning notice-spaced",
            role: "status",
            text: t.t("order.below_minimum"),
          })
        : null,
    );
  }

  /**
   * What an anonymous visitor sees in place of the items.
   *
   * The count and a sentence, not a blurred list: the API never sent the items,
   * so there is nothing to hide (F1.2, docs/06_ui_ux.md).
   */
  function anonymousCard(): HTMLElement {
    return section(
      t.t("order.item_list"),
      el("p", {
        text: t.t("order.items_so_far", { count: ctx.order.item_count }),
      }),
      el("p", { class: "muted", text: t.t("order.anonymous_hint") }),
      el(
        "p",
        {},
        el("a", {
          class: "link",
          href: loginHref(currentPath()),
          text: t.t("order.join"),
        }),
      ),
    );
  }

  // --- adding and editing an order item ------------------------------------

  /**
   * The add-item dialog.
   *
   * The same dialog edits an existing item: quantity, the menu item's
   * predefined options as checkboxes with their price deltas, a free-text note
   * and a line total that follows what is ticked (docs/06_ui_ux.md).
   */
  async function openItemEditor(
    existing: OrderItem | null,
    menuItem?: MenuItem,
  ): Promise<void> {
    const listed =
      menuItem ??
      ctx.menuItems.find(
        (candidate) => candidate.id === existing?.menu_item_id,
      );
    if (!listed) {
      return;
    }

    // The menu list leaves the options out -- they belong to one item, and
    // sending every item's options to draw a list would be most of a menu
    // nobody is looking at. The dialog is about to show them, so it fetches
    // the one item it needs.
    let source = listed;
    try {
      source = await api.get<MenuItem>(
        `/restaurants/${ctx.order.restaurant_id}/menu-items/${listed.id}`,
      );
    } catch {
      // Offer the item without its options rather than not at all: a person
      // can still order it and type what they want in the note.
      source = listed;
    }

    const quantity = input({
      type: "number",
      min: "1",
      step: "1",
      value: String(existing?.quantity ?? 1),
    });
    const note = textarea({
      value: existing?.note ?? "",
      rows: 2,
      placeholder: t.t("item.note.placeholder"),
    });

    const chosen = new Set(
      (existing?.modifications ?? [])
        .map((modification) => modification.modification_id)
        .filter((value): value is string => value !== null),
    );

    const total = el("p", { class: "line-total" });
    const boxes: { id: string; delta: number; box: HTMLInputElement }[] = [];

    const updateTotal = (): void => {
      const count = Math.max(1, Number.parseInt(quantity.value, 10) || 1);
      const deltas = boxes
        .filter((entry) => entry.box.checked)
        .reduce((sum, entry) => sum + entry.delta, 0);
      total.textContent = `${t.t("item.line_total")}: ${formatMoney(
        app.language,
        count * (source.price_cents + deltas),
        ctx.order.currency_code,
        moneyFormat,
      )}`;
    };

    const options = el("div", { class: "rows" });
    const modifications = source.modifications ?? [];
    if (modifications.length === 0) {
      options.appendChild(
        el("p", { class: "muted", text: t.t("menu.modification.none") }),
      );
    }
    for (const modification of modifications) {
      const label = `${modification.name} (${
        modification.price_delta_cents >= 0 ? "+" : ""
      }${moneyInputValue(modification.price_delta_cents, moneyFormat)})`;
      const row = checkbox(label, {
        checked: chosen.has(modification.id),
        onchange: updateTotal,
      });
      const box = row.querySelector("input");
      if (box) {
        boxes.push({
          id: modification.id,
          delta: modification.price_delta_cents,
          box,
        });
      }
      options.appendChild(row);
    }

    quantity.addEventListener("input", updateTotal);
    updateTotal();

    openFormDialog({
      t,
      title: source.name,
      focus: quantity,
      fields: [
        el("p", { class: "muted", text: source.description }),
        field({ label: t.t("item.quantity"), control: quantity }),
        el(
          "fieldset",
          { class: "fieldset" },
          el("legend", { text: t.t("item.modifications") }),
          options,
        ),
        field({
          label: t.t("item.note"),
          control: note,
          optionalLabel: t.t("auth.optional"),
        }),
        total,
      ],
      onSubmit: async () => {
        const count = Number.parseInt(quantity.value, 10);
        if (!Number.isFinite(count) || count < 1) {
          quantity.focus();
          return false;
        }
        const modificationIDs = boxes
          .filter((entry) => entry.box.checked)
          .map((entry) => entry.id);
        const noteText = note.value.trim();
        if (existing) {
          await api.patch(`/orders/${id}/items/${existing.id}`, {
            quantity: count,
            note: noteText,
            modification_ids: modificationIDs,
          });
        } else {
          await api.post(`/orders/${id}/items`, {
            menu_item_id: source.id,
            quantity: count,
            note: noteText,
            modification_ids: modificationIDs,
          });
        }
        return true;
      },
      after: refresh,
    });
  }

  async function removeItem(item: OrderItem): Promise<void> {
    const agreed = await confirmDialog({
      t,
      message: t.t("confirm.delete_item"),
      detail: item.item_name,
      confirmLabel: t.t("action.delete"),
    });
    if (!agreed) {
      return;
    }
    try {
      await api.delete(`/orders/${id}/items/${item.id}`);
      await refresh();
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  return { itemsCard, totalsCard, anonymousCard, openItemEditor };
}
