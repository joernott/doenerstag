// The menu editor: categories and items.
//
// Ordering follows F4.6 and is the server's: a printed menu's order, by
// numeric-aware item number and then by name. The page renders what it is given
// rather than sorting again, so the two can never disagree.
//
// The item editor itself lives in components/menuitem.ts, because the order
// page opens the same dialog when somebody adds a dish that is missing from the
// menu (F2 and docs/06_ui_ux.md).

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el, replace, type Child } from "../dom";
import { formatMoney } from "../format";
import { button, field, input } from "../components/forms";
import { thumbnailURL } from "../components/images";
import { openMenuItemEditor, type Category, type MenuItem } from "../components/menuitem";
import { confirmDialog } from "../components/modal";
import { referenceName, tagName } from "../i18n";
import { minorUnitOf, type ReferenceData } from "../reference";
import type { Restaurant } from "./restaurant";
import { actions, section, statusLine } from "./page";

/** The marks an item carries: its tags, allergens and additives, as chips. */
export function itemChips(app: App, item: MenuItem): HTMLElement[] {
  const { t } = app;
  return [
    ...item.tags.map((tag) => chip(tagName(t, tag), "chip-tag")),
    ...item.allergens.map((allergen) =>
      chip(referenceName(t, "allergen", allergen.code), "chip-allergen"),
    ),
    ...item.additives.map((additive) =>
      chip(referenceName(t, "additive", additive.code), "chip-additive"),
    ),
  ];
}

function chip(text: string, className: string): HTMLElement {
  return el("span", { class: `chip ${className}`, text });
}

/**
 * The menu, as a section of the restaurant page.
 *
 * Loaded after the page renders rather than before it: the restaurant's own
 * data is what somebody came for, and a menu of two hundred items should not
 * delay it.
 */
export function menuSection(
  app: App,
  reference: ReferenceData,
  restaurant: Restaurant,
): HTMLElement {
  const { t } = app;
  const status = statusLine();
  const body = el("div", { class: "menu-editor" });
  const minorUnit = minorUnitOf(reference.currencies, restaurant.currency_code);

  let categories: Category[] = [];
  let items: MenuItem[] = [];

  async function reload(): Promise<void> {
    try {
      [categories, items] = await Promise.all([
        getList<Category>(`/restaurants/${restaurant.id}/categories`, "categories"),
        getList<MenuItem>(`/restaurants/${restaurant.id}/menu-items`, "menu_items"),
      ]);
      render();
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  function edit(item: MenuItem | null, categoryID: string | null): void {
    openMenuItemEditor({
      app,
      reference,
      restaurantID: restaurant.id,
      categories,
      minorUnit,
      item,
      categoryID,
      onSaved: reload,
    });
  }

  function render(): void {
    const groups: Child[] = [];

    for (const [index, category] of categories.entries()) {
      groups.push(
        categoryBlock(
          category,
          index,
          items.filter((item) => item.category_id === category.id),
        ),
      );
    }

    // Items with no category come last, under their own heading, and only when
    // there are any. A restaurant with no categories at all gets one flat list
    // with no heading, which is what F4.5 asks for.
    const loose = items.filter((item) => item.category_id === null);
    if (loose.length > 0) {
      groups.push(
        categories.length === 0
          ? itemList(loose, null)
          : el(
              "div",
              { class: "menu-group" },
              el("h3", { class: "menu-group-title", text: t.t("menu.category.none") }),
              itemList(loose, null),
            ),
      );
    }

    if (groups.length === 0) {
      groups.push(el("p", { class: "muted", text: t.t("menu.empty") }));
    }

    replace(body, ...groups);
  }

  // --- categories ------------------------------------------------------------

  function categoryBlock(category: Category, index: number, own: MenuItem[]): HTMLElement {
    const name = input({ value: category.name });

    const rename = async (): Promise<void> => {
      status.clear();
      try {
        await api.patch(`/restaurants/${restaurant.id}/categories/${category.id}`, {
          name: name.value.trim(),
        });
        category.name = name.value.trim();
        status.say(t.t("state.saved"));
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    };

    // Reordering by button, not by drag handle. docs/06_ui_ux.md requires a
    // keyboard equivalent for any drag, and once the buttons exist for a list
    // that is rarely more than five entries long, the drag is only the part
    // that can break.
    const move = async (direction: -1 | 1): Promise<void> => {
      const other = categories[index + direction];
      if (!other) {
        return;
      }
      status.clear();
      try {
        await Promise.all([
          api.patch(`/restaurants/${restaurant.id}/categories/${category.id}`, {
            sort_order: other.sort_order,
          }),
          api.patch(`/restaurants/${restaurant.id}/categories/${other.id}`, {
            sort_order: category.sort_order,
          }),
        ]);
        await reload();
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    };

    const remove = async (): Promise<void> => {
      const agreed = await confirmDialog({
        t,
        message: t.t("menu.category.delete.confirm"),
        detail: category.name,
        confirmLabel: t.t("action.delete"),
      });
      if (!agreed) {
        return;
      }
      try {
        await api.delete(`/restaurants/${restaurant.id}/categories/${category.id}`);
        await reload();
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    };

    const controls = actions(
      button({
        label: t.t("action.save"),
        onclick: () => {
          void rename();
        },
      }),
      button({
        label: t.t("menu.category.move_up"),
        variant: "quiet",
        disabled: index === 0,
        onclick: () => {
          void move(-1);
        },
      }),
      button({
        label: t.t("menu.category.move_down"),
        variant: "quiet",
        disabled: index === categories.length - 1,
        onclick: () => {
          void move(1);
        },
      }),
      app.session.isAdmin
        ? button({
            label: t.t("menu.category.delete"),
            variant: "danger",
            onclick: () => {
              void remove();
            },
          })
        : null,
    );

    return el(
      "div",
      { class: "menu-group" },
      el(
        "div",
        { class: "menu-group-header" },
        field({ label: t.t("menu.category.name"), control: name }),
        controls,
      ),
      itemList(own, category.id),
    );
  }

  // --- items -----------------------------------------------------------------

  function itemList(own: MenuItem[], categoryId: string | null): HTMLElement {
    const rows = own.map((item) => itemRow(item));

    // An "add" button at the end of every category, so a missing item can be
    // added where it belongs rather than at the bottom and then moved.
    rows.push(
      el(
        "li",
        { class: "menu-add" },
        button({
          label: t.t("item.add"),
          onclick: () => {
            edit(null, categoryId);
          },
        }),
      ),
    );

    return el("ul", { class: "plain-list menu-items" }, ...rows);
  }

  function itemRow(item: MenuItem): HTMLElement {
    const marks = itemChips(app, item);

    return el(
      "li",
      { class: `menu-item ${item.available ? "" : "menu-item-unavailable"}`.trim() },
      item.image_id
        ? el("img", {
            class: "menu-thumb",
            src: thumbnailURL(item.image_id),
            alt: "",
            loading: "lazy",
          })
        : el("div", { class: "menu-thumb menu-thumb-empty", "aria-hidden": "true" }),
      el(
        "div",
        { class: "menu-item-text" },
        el(
          "div",
          { class: "menu-item-head" },
          item.external_id ? el("span", { class: "menu-number", text: item.external_id }) : null,
          el("strong", { text: item.name }),
          // Never colour or a strike-through alone: the state is also a word.
          item.available
            ? null
            : el("span", { class: "badge", text: t.t("menu.item.unavailable") }),
        ),
        item.description ? el("p", { class: "muted", text: item.description }) : null,
        marks.length > 0 ? el("div", { class: "chips" }, ...marks) : null,
      ),
      el("span", {
        class: "menu-price",
        text: formatMoney(app.language, item.price_cents, restaurant.currency_code, minorUnit),
      }),
      actions(
        button({
          label: t.t("action.edit"),
          onclick: () => {
            edit(item, item.category_id);
          },
        }),
      ),
    );
  }

  // --- adding a category -----------------------------------------------------

  const newCategory = input({});
  const addCategory = async (): Promise<void> => {
    status.clear();
    if (newCategory.value.trim() === "") {
      newCategory.focus();
      return;
    }
    try {
      await api.post(`/restaurants/${restaurant.id}/categories`, {
        name: newCategory.value.trim(),
        sort_order: (categories[categories.length - 1]?.sort_order ?? 0) + 10,
      });
      newCategory.value = "";
      await reload();
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  };

  void reload();

  return section(
    t.t("menu.items"),
    body,
    el(
      "div",
      { class: "row row-new" },
      field({ label: t.t("menu.category.name"), control: newCategory }),
      actions(
        button({
          label: t.t("menu.category.add"),
          onclick: () => {
            void addCategory();
          },
        }),
      ),
    ),
    status.element,
  );
}
