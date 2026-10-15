// The menu column: the dishes the kitchen will make at the order's time, with
// the filters, the folding categories, and a way to add or correct a dish.
// Part of the order page (20.29).

import { append, el, icon, keyed, rerender, type Child } from "../../dom";
import { formatMoney } from "../../format";
import { tagName } from "../../i18n";
import { button, checkbox, iconButton } from "../../components/forms";
import { openMenuItemEditor } from "../../components/menuitem";
import { menuItemParts } from "../../components/menurow";
import type { MenuItem } from "../../types";
import { actions, section } from "../page";
import type { OrderContext } from "./context";
import type { ItemEditor } from "./items";

export interface OrderMenu {
  /** Redraws the menu column from the context's menu. */
  renderMenu: () => void;
}

/** The menu column, drawn into `right`; ordering a dish opens `openItemEditor`. */
export function orderMenu(
  ctx: OrderContext,
  right: HTMLElement,
  openItemEditor: ItemEditor,
): OrderMenu {
  const { app, reference, moneyFormat, canOrder, reloadMenu } = ctx;
  const { t } = app;

  // --- the menu column -----------------------------------------------------

  const filters = {
    tags: new Set<string>(),
    allergens: new Set<string>(),
    additives: new Set<string>(),
    availableOnly: false,
  };
  // Per session and not persisted, exactly as docs/06_ui_ux.md asks.
  const collapsed = new Set<string>();
  // The filter groups somebody has opened, kept across rebuilds. A group used to
  // reopen only while one of its boxes was ticked, so unticking the last one
  // closed the group around the box that had focus (20.12).
  const openFilterGroups = new Set<string>();

  function renderMenu(): void {
    const groups: Child[] = [filterBar()];
    const shown = ctx.menuItems.filter(matchesFilter);

    for (const category of ctx.categoryList) {
      const own = shown.filter((item) => item.category_id === category.id);
      if (own.length === 0) {
        continue;
      }
      groups.push(categoryBlock(category.id, category.name, own));
    }

    const loose = shown.filter((item) => item.category_id === null);
    if (loose.length > 0) {
      groups.push(
        ctx.categoryList.length === 0
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
          keyed(
            button({
              label: t.t("order.add_missing_item"),
              onclick: () => {
                openMenuItemEditor({
                  app,
                  reference,
                  restaurantID: ctx.order.restaurant_id,
                  categories: ctx.categoryList,
                  money: moneyFormat,
                  item: null,
                  onSaved: reloadMenu,
                });
              },
            }),
            "add-missing",
          ),
        ),
      );
    }

    rerender(right, section(t.t("menu.items"), ...groups));
  }

  function matchesFilter(item: MenuItem): boolean {
    // What the kitchen will not make at this order's time is not on this menu
    // at all -- not greyed out, not behind a filter box. There is nothing to
    // decide about it: the order is for Tuesday and the pasta is a weekend
    // dish. The restaurant page still shows it, because that is the menu.
    // available_at is null when the menu was asked for without a time, which
    // the order page never does.
    if (item.available_at === false) {
      return false;
    }
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
          key: `filter:${entry.id}`,
          checked: chosen.has(entry.id),
          onchange: (event: Event) => {
            const target = event.target;
            if (target instanceof HTMLInputElement) {
              if (target.checked) {
                chosen.add(entry.id);
              } else {
                chosen.delete(entry.id);
              }
              // A group somebody is working in stays open, even once nothing in
              // it is ticked any more.
              openFilterGroups.add(legend);
              renderMenu();
            }
          },
        });
        list.appendChild(box);
      }

      const label = chosen.size > 0 ? `${legend} (${String(chosen.size)})` : legend;
      const details = el(
        "details",
        {
          class: "filter-group",
          open: chosen.size > 0 || openFilterGroups.has(legend),
        },
        el("summary", { text: label, "data-key": `filter-group:${legend}` }),
        list,
      );
      details.addEventListener("toggle", () => {
        if (details.open) {
          openFilterGroups.add(legend);
        } else {
          openFilterGroups.delete(legend);
        }
      });
      return details;
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
        key: "filter:available-only",
        checked: filters.availableOnly,
        onchange: (event: Event) => {
          const target = event.target;
          if (target instanceof HTMLInputElement) {
            filters.availableOnly = target.checked;
            renderMenu();
          }
        },
      }),
      keyed(
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
        "filter:clear",
      ),
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
        "data-key": `category:${id}`,
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
    const addable = canOrder() && item.available;
    // 19.4: a wrong price or a missing option is found while ordering, and is
    // put right here rather than on the restaurant page. The same editor and
    // the same people (docs/05: anybody signed in), and not tied to the order
    // being open, because it edits the restaurant's menu and not this order.
    // A line already in the order keeps its snapshot (ADR-0009).
    const editable = app.session.isAuthenticated && reference !== null;

    return el(
      "li",
      {
        class: `menu-item ${item.available ? "" : "menu-item-unavailable"}`.trim(),
      },
      ...menuItemParts(app, item),
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
          text: formatMoney(app.language, item.price_cents, ctx.order.currency_code, moneyFormat),
        }),
        editable || addable
          ? el(
              "div",
              { class: "menu-item-buttons" },
              editable
                ? keyed(
                    iconButton("pencil", `${t.t("action.edit")}: ${item.name}`, "", () => {
                      editMenuItem(item);
                    }),
                    `edit-dish:${item.id}`,
                  )
                : null,
              addable
                ? keyed(
                    button({
                      label: t.t("item.add"),
                      variant: "primary",
                      // Named for the dish: the menu column has one of these per item.
                      ariaLabel: `${t.t("item.add")}: ${item.name}`,
                      onclick: () => openItemEditor(null, item),
                    }),
                    `add:${item.id}`,
                  )
                : null,
            )
          : null,
      ),
    );
  }

  /** The restaurant page's menu-item editor, for one dish on this menu. */
  function editMenuItem(item: MenuItem): void {
    if (!reference) {
      return;
    }
    openMenuItemEditor({
      app,
      reference,
      restaurantID: ctx.order.restaurant_id,
      categories: ctx.categoryList,
      money: moneyFormat,
      item,
      categoryID: item.category_id,
      onSaved: reloadMenu,
    });
  }

  return { renderMenu };
}
