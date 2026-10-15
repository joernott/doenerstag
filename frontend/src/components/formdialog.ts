// A dialog whose body is a form: the shape of every editor that opens over a
// page.
//
// The seven such dialogs each built their own form, footer and buttons, and
// they had drifted apart: one linked its Save to the form with a click
// listener, one had no form at all so Enter did nothing, one submitted by
// clicking a hidden button, and two lacked the `form` attribute that connects a
// footer button to the form above it -- the reason 0.3.0's availability
// dialogs had a Save that did nothing (18.1). This owns all of that, so a
// dialog is its fields and what saving means (20.25).

import { el, type Child } from "../dom";
import type { Translator } from "../i18n";
import { button, form } from "./forms";
import { openModal, type ModalHandle } from "./modal";
import { runAction, statusLine, type StatusLine } from "./status";

export interface FormDialogOptions {
  t: Translator;
  /** The heading, which is also the dialog's accessible name. */
  title: string;
  /** The fields, in the form. The status line goes after them. */
  fields: Child[];
  /**
   * Does the work. A rejection is reported on the dialog's status line and
   * the dialog stays open; resolving to false also keeps it open, for a check
   * that has already said what is wrong.
   */
  onSubmit: (dialog: FormDialog) => Promise<unknown>;
  /**
   * Runs once the dialog has closed after a successful submit. Separate from
   * onSubmit because the page usually rebuilds itself here, and the dialog has
   * to have closed and given focus back to whatever opened it first: the
   * rebuild can then put focus on that control's replacement (20.12).
   */
  after?: () => void | Promise<void>;
  /** The submit button's label; Save by default. */
  submitLabel?: string;
  /** A destructive submit, Delete say, is coloured as one. */
  danger?: boolean;
  /** Buttons before Cancel in the footer, such as a Delete in an editor. */
  extraActions?: HTMLElement[];
  /**
   * The control focused when the dialog opens; the first field otherwise.
   * "cancel" starts on Cancel, as a destructive dialog should, so a stray
   * Enter does nothing it cannot take back.
   */
  focus?: HTMLElement | "cancel" | null;
  /** Extra classes on the dialog, for a wider one. */
  className?: string;
}

export interface FormDialog {
  readonly modal: ModalHandle;
  readonly status: StatusLine;
  readonly submit: HTMLButtonElement;
}

let sequence = 0;

export function openFormDialog(options: FormDialogOptions): FormDialog {
  const { t } = options;
  const status = statusLine();
  const submit = button({
    label: options.submitLabel ?? t.t("action.save"),
    variant: options.danger === true ? "danger" : "primary",
    type: "submit",
  });

  const body = form(
    () => {
      void send();
    },
    ...options.fields,
    status.element,
  );
  // The submit button sits in the footer, outside the form, and this is what
  // connects them.
  body.id = `form-dialog-${String(++sequence)}`;
  submit.setAttribute("form", body.id);

  const cancel = button({ label: t.t("action.cancel"), onclick: () => modal.close() });

  const modal = openModal({
    title: options.title,
    body,
    closeLabel: t.t("action.close"),
    actions: [
      ...(options.extraActions ?? []),
      cancel,
      submit,
    ],
    ...(options.className ? { className: options.className } : {}),
  });

  const dialog: FormDialog = { modal, status, submit };

  async function send(): Promise<void> {
    let keepOpen = false;
    const worked = await runAction({ status, t, busy: submit }, async () => {
      keepOpen = (await options.onSubmit(dialog)) === false;
    });
    if (!worked || keepOpen) {
      return;
    }
    modal.close();
    await options.after?.();
  }

  const first =
    options.focus === "cancel"
      ? cancel
      : (options.focus ?? body.querySelector<HTMLElement>("input, select, textarea"));
  first?.focus();
  return dialog;
}

/** A paragraph of explanation inside a dialog's fields. */
export function dialogHint(text: string): HTMLElement {
  return el("p", { class: "field-hint", text });
}
