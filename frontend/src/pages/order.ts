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

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { currentPath, loginHref } from "../returnto";
import { contactTarget } from "../contacts";
import { append, el, icon, type Child } from "../dom";
import {
  EVENT_ORDER_DELETED,
  EVENT_ORDER_EXPIRED,
  subscribeToOrder,
  type EventSourceFactory,
} from "../events";
import { formatDateTime, formatMoney, formatRelativeTime, moneyInputValue } from "../format";
import { button, checkbox, field, form, input, select, textarea } from "../components/forms";
import { thumbnailURL } from "../components/images";
import { openMenuItemEditor, type Category, type MenuItem } from "../components/menuitem";
import { confirmDialog, openModal } from "../components/modal";
import { minorUnitOf, referenceData } from "../reference";
import { tagName } from "../i18n";
import { itemChips } from "./menu";
import {
  isActive,
  summaryLink,
  type OrderDetail,
  type OrderHeader,
  type OrderItem,
} from "./orders";
import { actions, page, pageWithActions, section, statusLine } from "./page";
import type { Contact, Restaurant } from "./restaurant";

interface RestaurantDetail extends Restaurant {
  contacts: Contact[];
}

/** What the page needs from whoever is rendering it. */
export interface OrderPageOptions {
  /**
   * Registers the stream to be closed when this page is replaced.
   *
   * The router supplies it. Without one the stream would outlive the page, and
   * with the wrong one -- the router-wide list this used to use -- the render
   * that installs the page closes the stream it has just opened.
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
    return page(t.t("nav.orders"), el("p", { class: "field-error", text: errorMessage(t, error) }));
  }

  const [reference, restaurant, categories, menu] = await Promise.all([
    referenceData().catch(() => null),
    api.get<RestaurantDetail>(`/restaurants/${order.restaurant_id}`).catch(() => null),
    getList<Category>(`/restaurants/${order.restaurant_id}/categories`, "categories").catch(() => []),
    getList<MenuItem>(`/restaurants/${order.restaurant_id}/menu-items`, "menu_items").catch(() => []),
  ]);

  const minorUnit = minorUnitOf(reference?.currencies ?? [], order.currency_code);
  const status = statusLine();

  const left = el("div", { class: "order-column" });
  const right = el("div", { class: "order-column" });
  const banner = el("div");
  const heading = el("div", { class: "page-heading-actions" });

  let menuItems = menu;
  let categoryList = categories;

  // --- what the page knows -----------------------------------------------

  const detail = (): OrderDetail | null => ("items" in order ? order : null);

  const active = (): boolean => isActive(order);

  /** Whether this visitor may still change their own items (F6.1, F6.6). */
  const canOrder = (): boolean => app.session.isAuthenticated && active();

  /** Whether this visitor may change the order itself (F5.7). */
  const isCreator = (): boolean => {
    const own = detail();
    return own !== null && app.session.user?.id === own.creator_id;
  };

  /**
   * Whether this visitor may read the summary (F1.3).
   *
   * The creator, anybody with an item in the order, and the administrator. The
   * server decides for real; this decides whether the link is worth offering.
   */
  const isParticipant = (): boolean => {
    const own = detail();
    if (!own || !app.session.user) {
      return false;
    }
    const me = app.session.user.id;
    return (
      app.session.isAdmin ||
      own.creator_id === me ||
      own.items.some((item) => item.user_id === me)
    );
  };

  async function refresh(): Promise<void> {
    try {
      order = await api.get<OrderHeader | OrderDetail>(`/orders/${id}`);
      renderLeft();
      renderMenu();
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  async function reloadMenu(): Promise<void> {
    [categoryList, menuItems] = await Promise.all([
      getList<Category>(`/restaurants/${order.restaurant_id}/categories`, "categories"),
      getList<MenuItem>(`/restaurants/${order.restaurant_id}/menu-items`, "menu_items"),
    ]);
    renderMenu();
  }

  // --- the left column ----------------------------------------------------

  function renderLeft(): void {
    renderHeading();

    const own = detail();
    const parts: Child[] = [orderCard()];

    if (own) {
      parts.push(itemsCard(own), totalsCard(own));
    } else {
      parts.push(anonymousCard());
    }

    left.replaceChildren(...parts.filter((part): part is Node => part instanceof Node));
    banner.replaceChildren(
      active()
        ? el("span", { hidden: true })
        : el("p", { class: "notice", role: "status", text: t.t("order.closed_notice") }),
    );
  }

  function orderCard(): HTMLElement {
    const rows: [string, Child][] = [
      [t.t("restaurant.data"), restaurantLine()],
      [
        t.t("order.fulfilment.type"),
        t.t(order.fulfilment === "delivery" ? "order.fulfilment.delivery" : "order.fulfilment.pickup"),
      ],
      [t.t("order.fulfilment.label"), formatDateTime(app.language, order.fulfilment_at)],
      [
        t.t("order.deadline.label"),
        `${formatDateTime(app.language, order.deadline_at)} (${formatRelativeTime(
          app.language,
          order.deadline_at,
        )})`,
      ],
    ];

    if (order.money_collector) {
      rows.push([t.t("order.money_collector"), order.money_collector]);
    }
    if (order.pickup_person) {
      rows.push([t.t("order.pickup_person"), order.pickup_person]);
    }
    const own = detail();
    if (own) {
      rows.push([t.t("order.creator"), own.creator_name]);
    }
    if (order.min_order_value_cents !== null) {
      rows.push([
        t.t("order.minimum"),
        formatMoney(app.language, order.min_order_value_cents, order.currency_code, minorUnit),
      ]);
    }
    if (order.delivery_fee_cents !== null) {
      rows.push([
        t.t("order.delivery_fee"),
        formatMoney(app.language, order.delivery_fee_cents, order.currency_code, minorUnit),
      ]);
    }

    const list = el("dl", { class: "definitions" });
    for (const [label, value] of rows) {
      list.appendChild(el("dt", { text: label }));
      const dd = el("dd");
      append(dd, value);
      list.appendChild(dd);
    }

    // Editing and deleting the order live on the title's line with the summary,
    // not in this card: all three act on the order as a whole rather than on
    // anything inside the card, and they were the only reason it had a row of
    // buttons at all.
    return section(order.title, list, status.element);
  }

  /**
   * The controls on the title's line: summary, edit, delete.
   *
   * Rebuilt on every refresh rather than built once, because whether they apply
   * changes while the page is open. Adding the first item of your own makes you
   * a participant and unlocks the summary; removing your last one locks it
   * again; the deadline passing takes the edit away. Built once, the summary
   * stayed locked for the rest of the visit however many items you added.
   */
  function renderHeading(): void {
    const controls: Child[] = [summaryLink(app, id, isParticipant())];

    // F5.7: the creator edits while the order is active. F6.6 makes everything
    // read-only afterwards, for the administrator too, so there is nothing to
    // show then -- and a control that cannot be used is not shown at all.
    if (isCreator() && active()) {
      controls.push(
        button({
          label: t.t("order.edit"),
          onclick: () => {
            openOrderEditor();
          },
        }),
      );
    }
    if ((isCreator() || app.session.isAdmin) && detail()) {
      controls.push(
        button({
          label: t.t("order.delete"),
          variant: "danger",
          onclick: () => {
            void remove();
          },
        }),
      );
    }

    heading.replaceChildren(...controls.filter((part): part is Node => part instanceof Node));
  }

  /** The restaurant, with its contacts as the links they are meant to be. */
  function restaurantLine(): HTMLElement {
    const line = el("div", { class: "restaurant-line" });
    append(
      line,
      el("a", {
        class: "link",
        href: `/restaurants/${order.restaurant_id}`,
        text: order.restaurant_name,
      }),
    );

    for (const contact of restaurant?.contacts ?? []) {
      line.appendChild(contactLink(contact));
    }
    return line;
  }

  /**
   * One contact, as the link it is meant to be.
   *
   * Where it leads comes from contacts.ts, which is where that rule lives for
   * every page that shows one. This function used to decide for itself and had
   * drifted: an address fell through to the text branch because the list here
   * had never gained the case, a telephone number kept the spaces people write
   * it with, and a website typed without a scheme became a relative link.
   *
   * A contact with no label is its own label. Printing "value: value" -- which
   * is what the text branch did -- says the address twice and reads as though
   * something is missing.
   */
  function contactLink(contact: Contact): HTMLElement {
    const label = contact.label.trim();
    const target = contactTarget(contact);

    if (!target) {
      return el("span", {
        class: "contact muted",
        text: label ? `${label}: ${contact.value}` : contact.value,
      });
    }

    return el("a", {
      class: "link contact",
      href: target.href,
      ...(target.external ? { rel: "noreferrer", target: "_blank" } : {}),
      text: label || contact.value,
    });
  }

  /** The items, grouped by the person who ordered them. */
  function itemsCard(own: OrderDetail): HTMLElement {
    if (own.items.length === 0) {
      return section(t.t("order.item_list"), el("p", { class: "muted", text: t.t("order.empty") }));
    }

    const byPerson = new Map<string, OrderItem[]>();
    for (const item of own.items) {
      const group = byPerson.get(item.user_id) ?? [];
      group.push(item);
      byPerson.set(item.user_id, group);
    }

    const groups = [...byPerson].map(([userID, items]) => {
      const rows = items.map((item) => itemRow(item, userID));
      const personTotal = items.reduce((sum, item) => sum + item.line_total_cents, 0);

      return el(
        "div",
        { class: "person-group" },
        el(
          "h3",
          { class: "person-name" },
          items[0]?.user_name ?? "",
          el("span", {
            class: "person-total",
            text: formatMoney(app.language, personTotal, order.currency_code, minorUnit),
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
          text: item.modifications.map((modification) => modification.name).join(", "),
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
        extras.length > 0 ? el("div", { class: "order-item-extras" }, ...extras) : null,
      ),
      el("span", {
        class: "menu-price",
        text: formatMoney(app.language, item.line_total_cents, order.currency_code, minorUnit),
      }),
      // Own items only, and only while the order is open: F6.5 and F6.6.
      mine && canOrder()
        ? actions(
            button({
              label: t.t("action.edit"),
              onclick: () => {
                void openItemEditor(item);
              },
            }),
            button({
              label: t.t("action.delete"),
              variant: "danger",
              onclick: () => {
                void removeItem(item);
              },
            }),
          )
        : null,
    );
  }

  function totalsCard(own: OrderDetail): HTMLElement {
    const rows: [string, string][] = [
      [t.t("order.total"), formatMoney(app.language, own.item_total_cents, order.currency_code, minorUnit)],
    ];
    if (order.delivery_fee_cents) {
      rows.push([
        t.t("order.delivery_fee"),
        formatMoney(app.language, order.delivery_fee_cents, order.currency_code, minorUnit),
      ]);
    }

    const list = el("dl", { class: "definitions totals" });
    for (const [label, value] of rows) {
      list.appendChild(el("dt", { text: label }));
      list.appendChild(el("dd", { text: value }));
    }
    list.appendChild(el("dt", { class: "grand", text: t.t("order.total") }));
    list.appendChild(
      el("dd", {
        class: "grand",
        text: formatMoney(app.language, own.grand_total_cents, order.currency_code, minorUnit),
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
      el("p", { text: t.t("order.items_so_far", { count: order.item_count }) }),
      el("p", { class: "muted", text: t.t("order.anonymous_hint") }),
      el("p", {}, el("a", { class: "link", href: loginHref(currentPath()), text: t.t("order.join") })),
    );
  }

  // --- the order's own fields ---------------------------------------------

  function openOrderEditor(): void {
    const own = detail();
    if (!own) {
      return;
    }
    const editorStatus = statusLine();

    const fulfilment = select(
      [
        { value: "pickup", label: t.t("order.fulfilment.pickup") },
        { value: "delivery", label: t.t("order.fulfilment.delivery") },
      ],
      { value: order.fulfilment },
    );
    const fulfilmentAt = input({
      type: "datetime-local",
      value: localValue(order.fulfilment_at),
    });
    const deadlineAt = input({ type: "datetime-local", value: localValue(order.deadline_at) });
    const moneyCollector = input({ value: order.money_collector });
    const pickupPerson = input({ value: order.pickup_person });

    // F5.8: the restaurant is locked once the order has items, because the
    // prices and the currency were copied from it. Disabled with the reason
    // rather than hidden, or the absence would look like a bug.
    const restaurantNote =
      order.item_count > 0
        ? el("p", { class: "field-hint", text: t.t("order.restaurant_locked") })
        : null;

    const save = button({ label: t.t("action.save"), variant: "primary", type: "submit" });

    const body = form(
      () => {
        void store();
      },
      field({ label: t.t("order.fulfilment.type"), control: fulfilment }),
      field({ label: t.t("order.fulfilment.label"), control: fulfilmentAt }),
      field({ label: t.t("order.deadline.label"), control: deadlineAt }),
      restaurantNote,
      field({
        label: t.t("order.money_collector"),
        control: moneyCollector,
        optionalLabel: t.t("auth.optional"),
      }),
      field({
        label: t.t("order.pickup_person"),
        control: pickupPerson,
        optionalLabel: t.t("auth.optional"),
      }),
      editorStatus.element,
    );

    async function store(): Promise<void> {
      editorStatus.clear();
      const fulfils = new Date(fulfilmentAt.value);
      const closes = new Date(deadlineAt.value);
      if (Number.isNaN(fulfils.getTime()) || closes.getTime() >= fulfils.getTime()) {
        editorStatus.fail(t.t("order.deadline_before_fulfilment"));
        return;
      }

      save.disabled = true;
      try {
        await api.patch(`/orders/${id}`, {
          fulfilment: fulfilment.value,
          fulfilment_at: fulfils.toISOString(),
          deadline_at: closes.toISOString(),
          money_collector: moneyCollector.value.trim(),
          pickup_person: pickupPerson.value.trim(),
        });
        modal.close();
        await refresh();
      } catch (error) {
        editorStatus.fail(errorMessage(t, error));
      } finally {
        save.disabled = false;
      }
    }

    const modal = openModal({
      title: t.t("order.edit"),
      closeLabel: t.t("action.close"),
      className: "modal-narrow",
      body,
      actions: [button({ label: t.t("action.cancel"), onclick: () => modal.close() }), save],
    });
    body.id = `order-form-${id}`;
    save.setAttribute("form", body.id);
  }

  async function remove(): Promise<void> {
    const own = detail();
    // How many other people lose their items, named rather than described
    // (F5.9).
    const others = new Set(
      (own?.items ?? [])
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
      await api.delete(`/orders/${id}`);
      app.navigate("/");
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  // --- the menu column -----------------------------------------------------

  const filters = {
    tags: new Set<string>(),
    allergens: new Set<string>(),
    additives: new Set<string>(),
    availableOnly: false,
  };
  // Per session and not persisted, exactly as docs/06_ui_ux.md asks.
  const collapsed = new Set<string>();

  function renderMenu(): void {
    const groups: Child[] = [filterBar()];
    const shown = menuItems.filter(matchesFilter);

    for (const category of categoryList) {
      const own = shown.filter((item) => item.category_id === category.id);
      if (own.length === 0) {
        continue;
      }
      groups.push(categoryBlock(category.id, category.name, own));
    }

    const loose = shown.filter((item) => item.category_id === null);
    if (loose.length > 0) {
      groups.push(
        categoryList.length === 0
          ? el("ul", { class: "plain-list menu-items" }, ...loose.map((item) => menuRow(item)))
          : categoryBlock("", t.t("menu.category.none"), loose),
      );
    }

    if (shown.length === 0) {
      groups.push(el("p", { class: "muted", text: t.t("menu.empty") }));
    }

    // F2 and docs/06: a dish missing from the menu can be added without leaving
    // the order.
    if (app.session.isAuthenticated && reference) {
      groups.push(
        actions(
          button({
            label: t.t("order.add_missing_item"),
            onclick: () => {
              openMenuItemEditor({
                app,
                reference,
                restaurantID: order.restaurant_id,
                categories: categoryList,
                minorUnit,
                item: null,
                onSaved: reloadMenu,
              });
            },
          }),
        ),
      );
    }

    right.replaceChildren(section(t.t("menu.items"), ...groups));
  }

  function matchesFilter(item: MenuItem): boolean {
    if (filters.availableOnly && !item.available) {
      return false;
    }
    // Tags include, allergens and additives exclude. That asymmetry is the
    // point: people look for what they can eat by tag, and by what they must
    // avoid otherwise.
    if (filters.tags.size > 0 && !item.tags.some((tag) => filters.tags.has(tag.id))) {
      return false;
    }
    if (item.allergens.some((allergen) => filters.allergens.has(allergen.id))) {
      return false;
    }
    if (item.additives.some((additive) => filters.additives.has(additive.id))) {
      return false;
    }
    return true;
  }

  function filterBar(): HTMLElement {
    const bar = el("div", { class: "filter-bar" });
    if (!reference) {
      return bar;
    }

    /**
     * One filter, folded away until it is wanted.
     *
     * The three groups together are nearly forty checkboxes, and expanded they
     * sit between the top of the menu column and the first dish: a keyboard
     * user had to cross all of them to order anything. A <details> is three tab
     * stops instead of forty, is open to the same keyboard without any script,
     * and says in its own summary how many filters are active -- so folding one
     * away cannot hide the fact that it is filtering.
     */
    const group = (
      legend: string,
      entries: { id: string; label: string }[],
      chosen: Set<string>,
    ): HTMLElement => {
      const list = el("div", { class: "chips" });
      for (const entry of entries) {
        const box = checkbox(entry.label, {
          checked: chosen.has(entry.id),
          onchange: (event: Event) => {
            const target = event.target;
            if (target instanceof HTMLInputElement) {
              if (target.checked) {
                chosen.add(entry.id);
              } else {
                chosen.delete(entry.id);
              }
              renderMenu();
            }
          },
        });
        list.appendChild(box);
      }

      const label = chosen.size > 0 ? `${legend} (${String(chosen.size)})` : legend;
      return el(
        "details",
        { class: "filter-group", open: chosen.size > 0 },
        el("summary", { text: label }),
        list,
      );
    };

    append(
      bar,
      group(
        t.t("menu.filter.tags"),
        reference.tags.map((tag) => ({ id: tag.id, label: tagName(t, tag) })),
        filters.tags,
      ),
      group(
        t.t("menu.filter.exclude_allergens"),
        reference.allergens.map((allergen) => ({
          id: allergen.id,
          label: t.t(`allergen.${allergen.code}`),
        })),
        filters.allergens,
      ),
      group(
        t.t("menu.filter.exclude_additives"),
        reference.additives.map((additive) => ({
          id: additive.id,
          label: t.t(`additive.${additive.code}`),
        })),
        filters.additives,
      ),
      checkbox(t.t("menu.filter.available_only"), {
        checked: filters.availableOnly,
        onchange: (event: Event) => {
          const target = event.target;
          if (target instanceof HTMLInputElement) {
            filters.availableOnly = target.checked;
            renderMenu();
          }
        },
      }),
      button({
        label: t.t("menu.filter.clear"),
        variant: "quiet",
        onclick: () => {
          filters.tags.clear();
          filters.allergens.clear();
          filters.additives.clear();
          filters.availableOnly = false;
          renderMenu();
        },
      }),
    );
    return bar;
  }

  function categoryBlock(id: string, name: string, own: MenuItem[]): HTMLElement {
    const open = !collapsed.has(id);
    const list = el("ul", { class: "plain-list menu-items", hidden: !open });
    for (const item of own) {
      list.appendChild(menuRow(item));
    }

    const toggle = el(
      "button",
      {
        type: "button",
        class: "category-toggle",
        "aria-expanded": String(open),
        onclick: () => {
          if (collapsed.has(id)) {
            collapsed.delete(id);
          } else {
            collapsed.add(id);
          }
          renderMenu();
        },
      },
      // The same chevrons the restaurant page uses on its Menu tab. A plus and
      // a cross said "add" and "remove" on a page whose every other plus and
      // cross does exactly that; a chevron says "there is more underneath",
      // which is what this actually does.
      icon(open ? "chevron-down" : "chevron-right"),
      el("span", { text: name }),
      el("span", { class: "muted", text: ` (${String(own.length)})` }),
    );

    return el("div", { class: "menu-group" }, toggle, list);
  }

  function menuRow(item: MenuItem): HTMLElement {
    const marks = itemChips(app, item);
    const addable = canOrder() && item.available;

    return el(
      "li",
      { class: `menu-item ${item.available ? "" : "menu-item-unavailable"}`.trim() },
      item.image_id
        ? el("img", { class: "menu-thumb", src: thumbnailURL(item.image_id), alt: "", loading: "lazy" })
        : el("div", { class: "menu-thumb menu-thumb-empty", "aria-hidden": "true" }),
      el(
        "div",
        { class: "menu-item-text" },
        el(
          "div",
          { class: "menu-item-head" },
          item.external_id ? el("span", { class: "menu-number", text: item.external_id }) : null,
          el("strong", { text: item.name }),
          item.available ? null : el("span", { class: "badge", text: t.t("menu.item.unavailable") }),
        ),
        item.description ? el("p", { class: "muted", text: item.description }) : null,
        marks.length > 0 ? el("div", { class: "chips" }, ...marks) : null,
      ),
      // The price and the button share a column at the end of the row rather
      // than sitting beside each other in it. Side by side, the two of them
      // and the chips competed for one line: a dish with four allergens
      // squeezed the button until its label wrapped, and a column of buttons
      // that are one line tall next to some dishes and two next to others is
      // what made the list look broken. The column is `flex: none`, so the
      // chips can no longer take width from it.
      el(
        "div",
        { class: "menu-item-side" },
        el("span", {
          class: "menu-price",
          text: formatMoney(app.language, item.price_cents, order.currency_code, minorUnit),
        }),
        addable
          ? button({
              label: t.t("item.add"),
              variant: "primary",
              // Named for the dish: the menu column has one of these per item.
              ariaLabel: `${t.t("item.add")}: ${item.name}`,
              onclick: () => {
                void openItemEditor(null, item);
              },
            })
          : null,
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
  async function openItemEditor(existing: OrderItem | null, menuItem?: MenuItem): Promise<void> {
    const listed =
      menuItem ?? menuItems.find((candidate) => candidate.id === existing?.menu_item_id);
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
        `/restaurants/${order.restaurant_id}/menu-items/${listed.id}`,
      );
    } catch {
      // Offer the item without its options rather than not at all: a person
      // can still order it and type what they want in the note.
      source = listed;
    }

    const editorStatus = statusLine();

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
        order.currency_code,
        minorUnit,
      )}`;
    };

    const options = el("div", { class: "rows" });
    const modifications = source.modifications ?? [];
    if (modifications.length === 0) {
      options.appendChild(el("p", { class: "muted", text: t.t("menu.modification.none") }));
    }
    for (const modification of modifications) {
      const label = `${modification.name} (${
        modification.price_delta_cents >= 0 ? "+" : ""
      }${moneyInputValue(modification.price_delta_cents, minorUnit)})`;
      const row = checkbox(label, {
        checked: chosen.has(modification.id),
        onchange: updateTotal,
      });
      const box = row.querySelector("input");
      if (box) {
        boxes.push({ id: modification.id, delta: modification.price_delta_cents, box });
      }
      options.appendChild(row);
    }

    quantity.addEventListener("input", updateTotal);
    updateTotal();

    const save = button({ label: t.t("action.save"), variant: "primary", type: "submit" });

    async function store(): Promise<void> {
      editorStatus.clear();
      const count = Number.parseInt(quantity.value, 10);
      if (!Number.isFinite(count) || count < 1) {
        quantity.focus();
        return;
      }

      const payload = {
        menu_item_id: source.id,
        quantity: count,
        note: note.value.trim(),
        modification_ids: boxes.filter((entry) => entry.box.checked).map((entry) => entry.id),
      };

      save.disabled = true;
      try {
        if (existing) {
          await api.patch(`/orders/${id}/items/${existing.id}`, {
            quantity: payload.quantity,
            note: payload.note,
            modification_ids: payload.modification_ids,
          });
        } else {
          await api.post(`/orders/${id}/items`, payload);
        }
        modal.close();
        await refresh();
      } catch (error) {
        editorStatus.fail(errorMessage(t, error));
      } finally {
        save.disabled = false;
      }
    }

    const body = form(
      () => {
        void store();
      },
      el("p", { class: "muted", text: source.description }),
      field({ label: t.t("item.quantity"), control: quantity }),
      el("fieldset", { class: "fieldset" }, el("legend", { text: t.t("item.modifications") }), options),
      field({ label: t.t("item.note"), control: note, optionalLabel: t.t("auth.optional") }),
      total,
      editorStatus.element,
    );

    const modal = openModal({
      title: source.name,
      closeLabel: t.t("action.close"),
      body,
      actions: [button({ label: t.t("action.cancel"), onclick: () => modal.close() }), save],
    });
    body.id = `order-item-form-${existing?.id ?? "new"}`;
    save.setAttribute("form", body.id);
    quantity.focus();
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

  // --- live updates --------------------------------------------------------

  let stream = subscribeToOrder(
    id,
    {
      onEvent: (name) => {
        if (name === EVENT_ORDER_DELETED) {
          // Nothing left to look at: say so and go back to the overview.
          app.announce(t.t("order.deleted"));
          left.replaceChildren(
            section(order.title, el("p", { text: t.t("order.deleted") })),
          );
          stream.close();
          return;
        }

        // Every other event is answered by re-fetching. The payloads carry
        // enough to patch the DOM in place, and doing so would mean a second
        // implementation of "what an order looks like" that could drift from
        // the first. One fetch on a local network is cheaper than that risk.
        void refresh().then(() => {
          app.announce(
            name === EVENT_ORDER_EXPIRED ? t.t("order.closed_notice") : t.t("order.live.changed"),
          );
        });
      },
      onReconnect: () => {
        // Whatever happened while the stream was down was never delivered.
        void refresh();
      },
    },
    factory,
  );

  // Logging in or out changes what the stream is allowed to send, so the
  // subscription is made again -- and the page is re-fetched, because the shape
  // of the answer changes too.
  const unsubscribe = app.session.subscribe(() => {
    stream.close();
    stream = subscribeToOrder(id, { onEvent: () => void refresh(), onReconnect: () => void refresh() }, factory);
    void refresh();
  });

  // A stream outlives the DOM it feeds unless somebody closes it, and this page
  // cannot see that it has been navigated away from. The router can, so it is
  // the router that says when.
  options.cleanup?.onCleanup(() => {
    stream.close();
    unsubscribe();
  });

  renderLeft();
  renderMenu();

  return pageWithActions(
    order.title,
    heading,
    banner,
    el("div", { class: "order-layout" }, left, right),
  );
}

/** An API timestamp as a `datetime-local` value in the viewer's time zone. */
function localValue(iso: string): string {
  const date = new Date(iso);
  const pad = (value: number): string => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}`
  );
}
