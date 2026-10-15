// The restaurant's own fields. Part of the restaurant page.

import type { App } from "../../app";
import { api, errorMessage } from "../../api";
import { moneyInputValue, parseMoney } from "../../format";
import { field, form, input, textarea } from "../../components/forms";
import { dirtyTracker, type EditableSection } from "../../components/editable";
import { confirmDialog } from "../../components/modal";
import { imageField } from "../../components/images";
import { moneyFormatOf, type ReferenceData } from "../../reference";
import { card } from "../page";
import type { RestaurantDetail } from "../../types";
import { statusLine } from "../../components/status";
import { currencySelect } from "./selects";

/**
 * The restaurant's own fields, and the two operations that act on the whole of
 * it.
 *
 * Save and Delete are handed back rather than rendered here: they live on the
 * page heading, so that they are reachable whichever tab is open. What the
 * section keeps is the knowledge of *when* saving is worth offering, which is
 * why it reports its dirty state rather than exposing its inputs.
 */
export interface DataSection extends EditableSection {
  remove(): Promise<void>;
  /** Called with the new name after a save that changed it. */
  onRename(listen: (name: string) => void): void;
}

export function dataSection(
  app: App,
  reference: ReferenceData,
  restaurant: RestaurantDetail,
): DataSection {
  const { t } = app;
  const status = statusLine();
  let moneyFormat = moneyFormatOf(reference.currencies, restaurant.currency_code);

  const name = input({ name: "name", value: restaurant.name, required: true });
  const currency = currencySelect(app, reference, restaurant.currency_code);
  const minimum = input({
    name: "min_order_value",
    inputMode: "decimal",
    value:
      restaurant.min_order_value_cents === null
        ? ""
        : moneyInputValue(restaurant.min_order_value_cents, moneyFormat),
  });
  const fee = input({
    name: "delivery_fee",
    inputMode: "decimal",
    value:
      restaurant.delivery_fee_cents === null
        ? ""
        : moneyInputValue(restaurant.delivery_fee_cents, moneyFormat),
  });
  const notes = textarea({ name: "notes", value: restaurant.notes, rows: 3 });

  let logoImageId = restaurant.logo_image_id;
  const logo = imageField({
    t,
    locale: app.language,
    label: t.t("restaurant.logo"),
    imageId: logoImageId,
    limits: app.version,
    onChange: (id) => {
      logoImageId = id;
      touched();
    },
  });

  // --- has anything changed? ---------------------------------------------
  //
  // Compared against a snapshot rather than tracked with a flag, so typing a
  // character and deleting it again leaves Save disabled: "dirty" should mean
  // the form differs from what was loaded, not that somebody touched a key.
  const snapshot = (): string =>
    JSON.stringify([
      name.value.trim(),
      currency.value,
      minimum.value.trim(),
      fee.value.trim(),
      notes.value.trim(),
      logoImageId,
    ]);

  const tracker = dirtyTracker(snapshot);
  const renameListeners: ((name: string) => void)[] = [];
  const renamed = (next: string): void => {
    for (const listen of renameListeners) {
      listen(next);
    }
  };
  const touched = tracker.touched;

  for (const control of [name, currency, minimum, fee, notes]) {
    control.addEventListener("input", touched);
    control.addEventListener("change", touched);
  }

  // Changing the currency changes what the two money fields mean, so they are
  // re-rendered in the new currency's minor unit rather than silently keeping
  // digits that meant something else.
  currency.addEventListener("change", () => {
    const next = moneyFormatOf(reference.currencies, currency.value);
    for (const money of [minimum, fee]) {
      const parsed = parseMoney(money.value, moneyFormat);
      money.value = parsed === null ? "" : moneyInputValue(parsed, next);
    }
    moneyFormat = next;
  });

  async function save(): Promise<void> {
    status.clear();
    try {
      await api.patch(`/restaurants/${restaurant.id}`, {
        name: name.value.trim(),
        currency_code: currency.value,
        logo_image_id: logoImageId,
        min_order_value_cents: parseMoney(minimum.value, moneyFormat),
        delivery_fee_cents: parseMoney(fee.value, moneyFormat),
        notes: notes.value.trim(),
      });
      status.say(t.t("state.saved"));
      // The heading above the form as well as the browser tab. Setting only the
      // tab would leave the page displaying the old name until it was reloaded,
      // which reads as a save that did not take.
      renamed(name.value.trim());
      // What was just written becomes the new baseline, so Save goes quiet
      // again until something else changes.
      tracker.saved();
    } catch (error) {
      status.fail(errorMessage(t, error));
      // Rethrown, as the hours section does: the heading's Save is re-enabled
      // by a failure that reaches it, and one swallowed here would leave it
      // disabled until a field was edited.
      throw error;
    }
  }

  async function remove(): Promise<void> {
    const agreed = await confirmDialog({
      t,
      message: t.t("restaurant.delete.confirm"),
      detail: restaurant.name,
      confirmLabel: t.t("action.delete"),
    });
    if (!agreed) {
      return;
    }
    try {
      await api.delete(`/restaurants/${restaurant.id}`);
      app.navigate("/restaurants");
    } catch (error) {
      // 4003 when an order still references it, which is a real answer and not
      // a failure of the page: the message says so.
      status.fail(errorMessage(t, error));
    }
  }

  const element = card(
    // Still a form, so Enter in a field saves: the button that submits it is on
    // the page heading rather than in here, which a form is perfectly happy
    // with.
    form(
      () => {
        save().catch(() => {
          // Reported in the status line already; save rethrows only for the
          // heading's button, which has to know to re-enable itself.
        });
      },
      field({ label: t.t("restaurant.name"), control: name }),
      field({ label: t.t("restaurant.currency"), control: currency }),
      logo.element,
      field({
        label: t.t("restaurant.min_order_value"),
        control: minimum,
        optionalLabel: t.t("auth.optional"),
      }),
      field({
        label: t.t("restaurant.delivery_fee"),
        control: fee,
        optionalLabel: t.t("auth.optional"),
      }),
      field({
        label: t.t("restaurant.notes"),
        control: notes,
        optionalLabel: t.t("auth.optional"),
      }),
      status.element,
    ),
  );

  return {
    element,
    save,
    remove,
    onDirtyChange: tracker.onDirtyChange,
    onRename(listen: (name: string) => void): void {
      renameListeners.push(listen);
    },
  };
}

/** A currency selector, showing each currency's translated name and symbol. */
