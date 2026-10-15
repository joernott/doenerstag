// Deleting an account, from the account page or by the administrator.
//
// Both ask the server what the deletion would cost, list it, and have the
// person type the account's name to confirm -- an irreversible step taken by a
// misclick is what this prevents (docs/06_ui_ux.md). The two pages each built
// this dialog and its list of consequences, with their own copy of the impact
// type (20.28).

import { el } from "../dom";
import type { Translator } from "../i18n";
import type { DeletionImpact } from "../types";
import { openFormDialog } from "./formdialog";
import { field, fieldError, input } from "./forms";

/** What deleting the account would do, one consequence per line. */
export function impactList(t: Translator, impact: DeletionImpact): HTMLElement {
  const lines: string[] = [];
  if (impact.active_items > 0) {
    lines.push(t.t("account.delete.impact.active_items", { count: impact.active_items }));
  }
  if (impact.expired_items > 0) {
    lines.push(t.t("account.delete.impact.expired_items", { count: impact.expired_items }));
  }
  if (impact.created_active_orders > 0) {
    lines.push(t.t("account.delete.impact.orders", { count: impact.created_active_orders }));
  }
  if (lines.length === 0) {
    return el("p", { class: "muted", text: t.t("account.delete.impact.none") });
  }
  return el("ul", { class: "impact" }, ...lines.map((line) => el("li", { text: line })));
}

export interface DeleteAccountOptions {
  t: Translator;
  /** The dialog's heading. */
  title: string;
  /** The delete button; the heading by default. */
  submitLabel?: string;
  /** What is said before the consequences. */
  intro: string;
  impact: DeletionImpact;
  /** The name that has to be typed. */
  name: string;
  /** The label of the field it is typed into. */
  confirmLabel: string;
  /** What is said when the typed name is not the account's. */
  mismatch: string;
  /** Does the deleting. */
  remove: () => Promise<unknown>;
  /** Runs once the dialog has closed after the account went. */
  after: () => void | Promise<void>;
}

export function openDeleteAccountDialog(options: DeleteAccountOptions): void {
  const { t } = options;
  const typed = input({ name: "confirm_name", autocomplete: "off" });
  const mismatch = fieldError(typed);

  openFormDialog({
    t,
    title: options.title,
    className: "modal-narrow",
    submitLabel: options.submitLabel ?? options.title,
    danger: true,
    // Cancel first: a stray Enter must not delete an account.
    focus: "cancel",
    fields: [
      el("p", { text: options.intro }),
      impactList(t, options.impact),
      field({ label: options.confirmLabel, control: typed, error: mismatch }),
    ],
    onSubmit: async () => {
      if (typed.value.trim() !== options.name) {
        mismatch.set(options.mismatch);
        typed.focus();
        return false;
      }
      mismatch.clear();
      await options.remove();
      return true;
    },
    after: options.after,
  });
}
