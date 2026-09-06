// The menu item editor.
//
// A dialog rather than an inline form: it is the same form whether an item is
// being created or changed, and a menu of forty items cannot have forty forms
// open at once. It lives in its own module because two screens open it -- the
// restaurant page, and the order page, where F2 asks for a missing item to be
// addable without leaving the order.

import type { App } from "../app";
import { api, errorMessage } from "../api";
import { el } from "../dom";
import { moneyInputValue, parseMoney } from "../format";
import { referenceName, tagName } from "../i18n";
import type { Classification, ReferenceData, Tag } from "../reference";
import { button, checkbox, field, form, input, select, textarea } from "./forms";
import { imageField } from "./images";
import { confirmDialog, openModal } from "./modal";
import { statusLine } from "../pages/page";

export interface Category {
  id: string;
  name: string;
  sort_order: number;
}

export interface Modification {
  id: string;
  name: string;
  price_delta_cents: number;
  sort_order: number;
}

export interface MenuItem {
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

export interface MenuItemEditorOptions {
  app: App;
  reference: ReferenceData;
  restaurantID: string;
  /** The categories to choose from. May be empty. */
  categories: readonly Category[];
  /** How many decimals the restaurant's currency has. */
  minorUnit: number;
  /** The item being changed, or null to create one. */
  item: MenuItem | null;
  /** Which category a new item lands in. */
  categoryID?: string | null;
  /** Called after a successful save or delete. */
  onSaved: () => void | Promise<void>;
  /** Whether the option editor is offered. Off where there is no room for it. */
  withModifications?: boolean;
}

/** Opens the editor. */
export function openMenuItemEditor(options: MenuItemEditorOptions): void {
  const { app, reference, restaurantID, categories, minorUnit, item } = options;
  const { t } = app;
  const status = statusLine();

  const name = input({ value: item?.name ?? "", required: true });
  const externalId = input({ value: item?.external_id ?? "" });
  const price = input({
    inputMode: "decimal",
    value: item ? moneyInputValue(item.price_cents, minorUnit) : "",
    required: true,
  });
  const description = textarea({ value: item?.description ?? "", rows: 2 });
  const available = checkbox(t.t("menu.item.available"), { checked: item?.available ?? true });
  const availableBox = available.querySelector("input");

  const category = select(
    [
      { value: "", label: t.t("menu.category.none") },
      ...categories.map((entry) => ({ value: entry.id, label: entry.name })),
    ],
    { value: item?.category_id ?? options.categoryID ?? "" },
  );

  let imageId = item?.image_id ?? null;
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
      checked: item?.tags.some((own) => own.id === tag.id) ?? false,
    })),
  );
  const allergens = checkboxGroup(
    t.t("menu.allergens"),
    reference.allergens.map((allergen) => ({
      id: allergen.id,
      label: referenceName(t, "allergen", allergen.code),
      checked: item?.allergens.some((own) => own.id === allergen.id) ?? false,
    })),
  );
  const additives = checkboxGroup(
    t.t("menu.additives"),
    reference.additives.map((additive) => ({
      id: additive.id,
      label: referenceName(t, "additive", additive.code),
      checked: item?.additives.some((own) => own.id === additive.id) ?? false,
    })),
  );

  const save = button({ label: t.t("action.save"), variant: "primary", type: "submit" });

  async function store(): Promise<void> {
    status.clear();
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
      if (item) {
        await api.patch(`/restaurants/${restaurantID}/menu-items/${item.id}`, payload);
      } else {
        await api.post(`/restaurants/${restaurantID}/menu-items`, payload);
      }
      modal.close();
      await options.onSaved();
    } catch (error) {
      status.fail(errorMessage(t, error));
    } finally {
      save.disabled = false;
    }
  }

  async function remove(): Promise<void> {
    if (!item) {
      return;
    }
    const agreed = await confirmDialog({
      t,
      message: t.t("menu.item.delete.confirm"),
      detail: item.name,
      confirmLabel: t.t("action.delete"),
    });
    if (!agreed) {
      return;
    }
    try {
      await api.delete(`/restaurants/${restaurantID}/menu-items/${item.id}`);
      modal.close();
      await options.onSaved();
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  const body = form(
    () => {
      void store();
    },
    field({
      label: t.t("menu.item.number"),
      control: externalId,
      optionalLabel: t.t("auth.optional"),
    }),
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
    item && options.withModifications !== false
      ? modificationsBlock(options, item)
      : null,
    status.element,
  );

  const modal = openModal({
    title: item ? t.t("menu.item.edit") : t.t("menu.item.new"),
    closeLabel: t.t("action.close"),
    body,
    actions: [
      button({ label: t.t("action.cancel"), onclick: () => modal.close() }),
      item && app.session.isAdmin
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

  // The save button sits in the dialog's footer, outside the form it submits.
  // `form` on the button is what connects the two.
  body.id = `menu-item-form-${item?.id ?? "new"}`;
  save.setAttribute("form", body.id);
  name.focus();
}

/**
 * The options belonging to one item.
 *
 * A flat list with a price delta each, per ADR-0007: no groups, no "choose
 * exactly one", because a doner shop's menu does not have them and the
 * machinery to express them would be larger than everything else here.
 */
function modificationsBlock(options: MenuItemEditorOptions, item: MenuItem): HTMLElement {
  const { app, restaurantID, minorUnit } = options;
  const { t } = app;
  const status = statusLine();
  const list = el("div", { class: "rows" });
  let modifications: Modification[] = item.modifications ?? [];

  function render(): void {
    const rows =
      modifications.length === 0
        ? [el("p", { class: "muted", text: t.t("menu.modification.none") })]
        : modifications.map((modification) => row(modification));
    list.replaceChildren(...rows, newRow());
  }

  function row(modification: Modification): HTMLElement {
    const name = input({ value: modification.name });
    const delta = input({
      inputMode: "decimal",
      value: moneyInputValue(modification.price_delta_cents, minorUnit),
    });

    const save = async (): Promise<void> => {
      status.clear();
      try {
        await api.patch(
          `/restaurants/${restaurantID}/menu-items/${item.id}/modifications/${modification.id}`,
          {
            name: name.value.trim(),
            price_delta_cents: parseMoney(delta.value, minorUnit) ?? 0,
          },
        );
        status.say(t.t("state.saved"));
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    };

    const remove = async (): Promise<void> => {
      status.clear();
      try {
        await api.delete(
          `/restaurants/${restaurantID}/menu-items/${item.id}/modifications/${modification.id}`,
        );
        modifications = modifications.filter((entry) => entry.id !== modification.id);
        render();
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    };

    return el(
      "div",
      { class: "row" },
      field({ label: t.t("menu.modification.name"), control: name }),
      field({ label: t.t("menu.modification.price"), control: delta }),
      actionsRow(
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
      status.clear();
      if (name.value.trim() === "") {
        name.focus();
        return;
      }
      try {
        const created = await api.post<Modification>(
          `/restaurants/${restaurantID}/menu-items/${item.id}/modifications`,
          {
            name: name.value.trim(),
            price_delta_cents: parseMoney(delta.value, minorUnit) ?? 0,
            sort_order: (modifications[modifications.length - 1]?.sort_order ?? 0) + 10,
          },
        );
        modifications = [...modifications, created];
        render();
      } catch (error) {
        status.fail(errorMessage(t, error));
      }
    };

    return el(
      "div",
      { class: "row row-new" },
      field({ label: t.t("menu.modification.name"), control: name }),
      field({ label: t.t("menu.modification.price"), control: delta }),
      actionsRow(
        button({
          label: t.t("menu.modification.add"),
          onclick: () => {
            void add();
          },
        }),
      ),
    );
  }

  // The list view leaves modifications out; the editor fetches them for the one
  // item it is editing.
  void api
    .get<MenuItem>(`/restaurants/${restaurantID}/menu-items/${item.id}`)
    .then((full) => {
      modifications = full.modifications ?? [];
      render();
    })
    .catch((error: unknown) => {
      status.fail(errorMessage(t, error));
    });

  render();
  return el(
    "fieldset",
    { class: "fieldset" },
    el("legend", { text: t.t("menu.modification.title") }),
    list,
    status.element,
  );
}

function actionsRow(...controls: (HTMLElement | null)[]): HTMLElement {
  const row = el("div", { class: "actions" });
  for (const control of controls) {
    if (control) {
      row.appendChild(control);
    }
  }
  return row;
}

interface CheckboxEntry {
  id: string;
  label: string;
  checked: boolean;
}

/**
 * A group of checkboxes in a fieldset, which is how a set of related choices is
 * announced as one thing rather than as fourteen unrelated ones.
 */
export function checkboxGroup(
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

  return {
    element: el("fieldset", { class: "fieldset" }, el("legend", { text: legend }), list),
    selected: () => [...boxes].filter(([, box]) => box.checked).map(([id]) => id),
  };
}
