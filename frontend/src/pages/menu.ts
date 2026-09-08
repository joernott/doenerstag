// The menu editor: categories, items, their classification and their options.
//
// Ordering follows F4.6 and is the server's: a printed menu's order, by
// numeric-aware item number and then by name. The page renders what it is given
// rather than sorting again, so the two can never disagree.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { el, replace, type Child } from "../dom";
import { formatMoney, moneyInputValue, parseMoney } from "../format";
import { button, checkbox, field, form, input, select, textarea } from "../components/forms";
import { imageField, thumbnailURL } from "../components/images";
import { confirmDialog, openModal } from "../components/modal";
import { referenceName, tagName } from "../i18n";
import { minorUnitOf, type Classification, type ReferenceData, type Tag } from "../reference";
import type { Restaurant } from "./restaurant";
import { actions, section, statusLine } from "./page";

interface Category {
  id: string;
  name: string;
  sort_order: number;
}

interface Modification {
  id: string;
  name: string;
  price_delta_cents: number;
  sort_order: number;
}

interface MenuItem {
  id: string;
  category_id: string | null;
  external_id: string;
  name: string;
  description: string;
  image_id: string | null;
  price_cents: number;
  available: boolean;
  tags: Tag[];
  allergens: Classification[];
  additives: Classification[];
  modifications?: Modification[];
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

    // Reordering by keyboard, not by drag handle. docs/06_ui_ux.md requires a
    // keyboard alternative to dragging; this application has only the
    // alternative, because two buttons are the whole feature and a drag
    // implementation would be the part that breaks.
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
            editItem(null, categoryId);
          },
        }),
      ),
    );

    return el("ul", { class: "plain-list menu-items" }, ...rows);
  }

  function itemRow(item: MenuItem): HTMLElement {
    const marks = [
      ...item.tags.map((tag) => chip(tagName(t, tag), "chip-tag")),
      ...item.allergens.map((allergen) =>
        chip(referenceName(t, "allergen", allergen.code), "chip-allergen"),
      ),
      ...item.additives.map((additive) =>
        chip(referenceName(t, "additive", additive.code), "chip-additive"),
      ),
    ];

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
            editItem(item, item.category_id);
          },
        }),
      ),
    );
  }

  function chip(text: string, className: string): HTMLElement {
    return el("span", { class: `chip ${className}`, text });
  }

  /**
   * The item editor.
   *
   * A modal, because it is the same form whether an item is being created or
   * changed, and because a menu of forty items cannot have forty forms open on
   * the page at once. Options are edited inside it once the item exists: a
   * modification belongs to an item, and there is nothing to attach one to
   * until the item has been saved.
   */
  function editItem(existing: MenuItem | null, categoryId: string | null): void {
    const modalStatus = statusLine();

    const name = input({ value: existing?.name ?? "", required: true });
    const externalId = input({ value: existing?.external_id ?? "" });
    const price = input({
      inputMode: "decimal",
      value: existing ? moneyInputValue(existing.price_cents, minorUnit) : "",
      required: true,
    });
    const description = textarea({ value: existing?.description ?? "", rows: 2 });
    const available = checkbox(t.t("menu.item.available"), {
      checked: existing?.available ?? true,
    });
    const availableBox = available.querySelector("input");

    const category = select(
      [
        { value: "", label: t.t("menu.category.none") },
        ...categories.map((entry) => ({ value: entry.id, label: entry.name })),
      ],
      { value: existing?.category_id ?? categoryId ?? "" },
    );

    let imageId = existing?.image_id ?? null;
    const picture = imageField({
      t,
      locale: app.language,
      label: t.t("menu.item.image"),
      imageId,
      limits: app.version,
      onChange: (id) => {
        imageId = id;
      },
    });

    const tags = checkboxGroup(
      t.t("menu.tags"),
      reference.tags.map((tag) => ({
        id: tag.id,
        label: tagName(t, tag),
        checked: existing?.tags.some((own) => own.id === tag.id) ?? false,
      })),
    );
    const allergens = checkboxGroup(
      t.t("menu.allergens"),
      reference.allergens.map((allergen) => ({
        id: allergen.id,
        label: referenceName(t, "allergen", allergen.code),
        checked: existing?.allergens.some((own) => own.id === allergen.id) ?? false,
      })),
    );
    const additives = checkboxGroup(
      t.t("menu.additives"),
      reference.additives.map((additive) => ({
        id: additive.id,
        label: referenceName(t, "additive", additive.code),
        checked: existing?.additives.some((own) => own.id === additive.id) ?? false,
      })),
    );

    const save = button({ label: t.t("action.save"), variant: "primary", type: "submit" });

    async function store(): Promise<void> {
      modalStatus.clear();
      const cents = parseMoney(price.value, minorUnit);
      if (cents === null) {
        price.focus();
        return;
      }

      const payload = {
        category_id: category.value === "" ? null : category.value,
        external_id: externalId.value.trim(),
        name: name.value.trim(),
        description: description.value.trim(),
        image_id: imageId,
        price_cents: cents,
        available: availableBox?.checked ?? true,
        tag_ids: tags.selected(),
        allergen_ids: allergens.selected(),
        additive_ids: additives.selected(),
      };

      save.disabled = true;
      try {
        if (existing) {
          await api.patch(`/restaurants/${restaurant.id}/menu-items/${existing.id}`, payload);
        } else {
          await api.post(`/restaurants/${restaurant.id}/menu-items`, payload);
        }
        modal.close();
        await reload();
      } catch (error) {
        modalStatus.fail(errorMessage(t, error));
      } finally {
        save.disabled = false;
      }
    }

    async function remove(): Promise<void> {
      if (!existing) {
        return;
      }
      const agreed = await confirmDialog({
        t,
        message: t.t("menu.item.delete.confirm"),
        detail: existing.name,
        confirmLabel: t.t("action.delete"),
      });
      if (!agreed) {
        return;
      }
      try {
        await api.delete(`/restaurants/${restaurant.id}/menu-items/${existing.id}`);
        modal.close();
        await reload();
      } catch (error) {
        modalStatus.fail(errorMessage(t, error));
      }
    }

    const body = form(
      () => {
        void store();
      },
      field({ label: t.t("menu.item.number"), control: externalId, optionalLabel: t.t("auth.optional") }),
      field({ label: t.t("restaurant.name"), control: name }),
      field({ label: t.t("menu.item.price"), control: price }),
      field({
        label: t.t("menu.item.description"),
        control: description,
        optionalLabel: t.t("auth.optional"),
      }),
      field({ label: t.t("menu.item.category"), control: category }),
      picture.element,
      available,
      tags.element,
      allergens.element,
      additives.element,
      existing ? modificationsBlock(existing) : null,
      modalStatus.element,
    );

    const modal = openModal({
      title: existing ? t.t("menu.item.edit") : t.t("menu.item.new"),
      closeLabel: t.t("action.close"),
      body,
      actions: [
        button({ label: t.t("action.cancel"), onclick: () => modal.close() }),
        existing && app.session.isAdmin
          ? button({
              label: t.t("action.delete"),
              variant: "danger",
              onclick: () => {
                void remove();
              },
            })
          : null,
        save,
      ].filter((control): control is HTMLButtonElement => control !== null),
    });

    // The footer's save button submits the form it is not inside, which is what
    // `form` on a button is for.
    body.id = body.id || `item-form-${existing?.id ?? "new"}`;
    save.setAttribute("form", body.id);
    name.focus();
  }

  // --- modifications ---------------------------------------------------------

  /**
   * The options belonging to one item.
   *
   * A flat list with a price delta each, per ADR-0007: no groups, no
   * "choose exactly one", because a doner shop's menu does not have them and
   * the machinery to express them would be larger than everything else here.
   */
  function modificationsBlock(item: MenuItem): HTMLElement {
    const listStatus = statusLine();
    const list = el("div", { class: "rows" });
    let modifications: Modification[] = item.modifications ?? [];

    function render(): void {
      const rows: Child[] =
        modifications.length === 0
          ? [el("p", { class: "muted", text: t.t("menu.modification.none") })]
          : modifications.map((modification) => row(modification));
      replace(list, ...rows, newRow());
    }

    function row(modification: Modification): HTMLElement {
      const name = input({ value: modification.name });
      const delta = input({
        inputMode: "decimal",
        value: moneyInputValue(modification.price_delta_cents, minorUnit),
      });

      const save = async (): Promise<void> => {
        listStatus.clear();
        try {
          await api.patch(
            `/restaurants/${restaurant.id}/menu-items/${item.id}/modifications/${modification.id}`,
            {
              name: name.value.trim(),
              price_delta_cents: parseMoney(delta.value, minorUnit) ?? 0,
            },
          );
          listStatus.say(t.t("state.saved"));
        } catch (error) {
          listStatus.fail(errorMessage(t, error));
        }
      };

      const remove = async (): Promise<void> => {
        listStatus.clear();
        try {
          await api.delete(
            `/restaurants/${restaurant.id}/menu-items/${item.id}/modifications/${modification.id}`,
          );
          modifications = modifications.filter((entry) => entry.id !== modification.id);
          render();
        } catch (error) {
          listStatus.fail(errorMessage(t, error));
        }
      };

      return el(
        "div",
        { class: "row" },
        field({ label: t.t("menu.modification.name"), control: name }),
        field({ label: t.t("menu.modification.price"), control: delta }),
        actions(
          button({
            label: t.t("action.save"),
            onclick: () => {
              void save();
            },
          }),
          app.session.isAdmin
            ? button({
                label: t.t("action.remove"),
                variant: "danger",
                onclick: () => {
                  void remove();
                },
              })
            : null,
        ),
      );
    }

    function newRow(): HTMLElement {
      const name = input({});
      const delta = input({ inputMode: "decimal", value: moneyInputValue(0, minorUnit) });

      const add = async (): Promise<void> => {
        listStatus.clear();
        if (name.value.trim() === "") {
          name.focus();
          return;
        }
        try {
          const created = await api.post<Modification>(
            `/restaurants/${restaurant.id}/menu-items/${item.id}/modifications`,
            {
              name: name.value.trim(),
              price_delta_cents: parseMoney(delta.value, minorUnit) ?? 0,
              sort_order: (modifications[modifications.length - 1]?.sort_order ?? 0) + 10,
            },
          );
          modifications = [...modifications, created];
          render();
        } catch (error) {
          listStatus.fail(errorMessage(t, error));
        }
      };

      return el(
        "div",
        { class: "row row-new" },
        field({ label: t.t("menu.modification.name"), control: name }),
        field({ label: t.t("menu.modification.price"), control: delta }),
        actions(
          button({
            label: t.t("menu.modification.add"),
            onclick: () => {
              void add();
            },
          }),
        ),
      );
    }

    // The list view leaves modifications out; the editor fetches them for the
    // one item it is editing.
    void api
      .get<MenuItem>(`/restaurants/${restaurant.id}/menu-items/${item.id}`)
      .then((full) => {
        modifications = full.modifications ?? [];
        render();
      })
      .catch((error: unknown) => {
        listStatus.fail(errorMessage(t, error));
      });

    render();
    return el(
      "fieldset",
      { class: "fieldset" },
      el("legend", { text: t.t("menu.modification.title") }),
      list,
      listStatus.element,
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

interface CheckboxEntry {
  id: string;
  label: string;
  checked: boolean;
}

/**
 * A group of checkboxes in a fieldset, which is how a set of related choices
 * is announced as one thing rather than as fourteen unrelated ones.
 */
function checkboxGroup(
  legend: string,
  entries: readonly CheckboxEntry[],
): { element: HTMLElement; selected: () => string[] } {
  const boxes = new Map<string, HTMLInputElement>();
  const list = el("div", { class: "checkbox-grid" });

  for (const entry of entries) {
    const row = checkbox(entry.label, { checked: entry.checked });
    const box = row.querySelector("input");
    if (box) {
      boxes.set(entry.id, box);
    }
    list.appendChild(row);
  }

  const element = el(
    "fieldset",
    { class: "fieldset" },
    el("legend", { text: legend }),
    list,
  );

  return {
    element,
    selected: () => [...boxes].filter(([, box]) => box.checked).map(([id]) => id),
  };
}
