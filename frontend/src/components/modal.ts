// Modals, and the confirmation dialog built on one.
//
// docs/06_ui_ux.md sets the rules: focus is trapped, Escape closes, focus
// returns to whatever opened it, and every destructive action is confirmed by a
// dialog that names what it is about to destroy.
//
// A native <dialog> would give the trap and the Escape handling for free. It is
// not used because its backdrop cannot be styled from the theme variables
// without ::backdrop rules that behave differently across the two browsers this
// application targets, and because a modal that closes on Escape *sometimes* --
// which is what happens when a nested control swallows the key -- is worse than
// one whose handling is visible in this file.

import { append, el, focusable, icon, type Child } from "../dom";
import type { Translator } from "../i18n";

/** A modal, once open. */
export interface ModalHandle {
  /** The dialog element, for a caller that needs to reach inside it. */
  readonly element: HTMLElement;
  /** Closes the modal and restores focus. Calling it twice is harmless. */
  close(): void;
}

export interface ModalOptions {
  /** The heading, which is also the dialog's accessible name. */
  title: string;
  /** The body content. */
  body: Child | Child[];
  /** Buttons for the footer, in reading order. */
  actions?: HTMLElement[];
  /** The label for the close button in the corner. */
  closeLabel: string;
  /** Called after the modal closes, however it closed. */
  onClose?: () => void;
  /** Extra classes on the dialog, for a wider or narrower one. */
  className?: string;
}

/** The open modals, innermost last. Only the innermost traps focus. */
const stack: ModalHandle[] = [];

let sequence = 0;

/** Opens a modal. */
export function openModal(options: ModalOptions): ModalHandle {
  const previouslyFocused = document.activeElement;
  const titleId = `modal-title-${++sequence}`;

  const dialog = el("div", {
    class: `modal ${options.className ?? ""}`.trim(),
    role: "dialog",
    "aria-modal": "true",
    "aria-labelledby": titleId,
  });

  const closeButton = el(
    "button",
    {
      type: "button",
      class: "icon-button modal-close",
      "aria-label": options.closeLabel,
      onclick: () => handle.close(),
    },
    icon("close"),
  );

  const header = el(
    "div",
    { class: "modal-header" },
    el("h2", { class: "modal-title", id: titleId, text: options.title }),
    closeButton,
  );

  const body = el("div", { class: "modal-body" });
  append(body, ...(Array.isArray(options.body) ? options.body : [options.body]));

  append(dialog, header, body);
  if (options.actions && options.actions.length > 0) {
    const footer = el("div", { class: "modal-actions" });
    append(footer, ...options.actions);
    dialog.appendChild(footer);
  }

  const backdrop = el(
    "div",
    {
      class: "modal-backdrop",
      // A click on the backdrop itself closes; a click that started inside the
      // dialog and drifted out does not, which is why the target is checked
      // rather than trusting the bubble.
      onclick: (event: Event) => {
        if (event.target === backdrop) {
          handle.close();
        }
      },
    },
    dialog,
  );

  let closed = false;
  const handle: ModalHandle = {
    element: dialog,
    close(): void {
      if (closed) {
        return;
      }
      closed = true;
      document.removeEventListener("keydown", onKeyDown, true);
      backdrop.remove();
      stack.splice(stack.indexOf(handle), 1);
      if (stack.length === 0) {
        document.body.classList.remove("modal-open");
        document.getElementById("app-root")?.removeAttribute("inert");
      }
      if (previouslyFocused instanceof HTMLElement) {
        previouslyFocused.focus();
      }
      options.onClose?.();
    },
  };

  function onKeyDown(event: KeyboardEvent): void {
    if (stack[stack.length - 1] !== handle) {
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      handle.close();
      return;
    }
    if (event.key !== "Tab") {
      return;
    }

    const stops = focusable(dialog);
    const first = stops[0];
    const last = stops[stops.length - 1];
    if (!first || !last) {
      event.preventDefault();
      return;
    }
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  document.addEventListener("keydown", onKeyDown, true);
  document.body.appendChild(backdrop);
  document.body.classList.add("modal-open");
  // The page behind a modal is not merely unreachable by Tab: it is inert, so
  // a screen reader's own cursor cannot wander into it either.
  document.getElementById("app-root")?.setAttribute("inert", "");
  stack.push(handle);

  // Focus the first control rather than the dialog itself, so the keyboard is
  // already where the person needs it. The close button is the fallback, and
  // there is always a close button.
  (focusable(dialog)[0] ?? closeButton).focus();

  return handle;
}

export interface ConfirmOptions {
  /** The translator, so the standard buttons are labelled in the interface language. */
  t: Translator;
  /** The heading. Defaults to "Are you sure?". */
  title?: string;
  /** What is about to happen, in one sentence. */
  message: string;
  /** The collateral effect, when there is one: "3 other people have items…". */
  detail?: string;
  /** The label on the confirming button. Defaults to "Confirm". */
  confirmLabel?: string;
  /** Whether the confirming button is styled as destructive. Defaults to true. */
  danger?: boolean;
}

/**
 * Asks for confirmation. Resolves true only if the person confirmed.
 *
 * Closing by any other route -- Escape, the backdrop, the corner button --
 * resolves false, because "get me out of this dialog" is never consent.
 */
export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  const { t } = options;

  return new Promise((resolve) => {
    let confirmed = false;

    const cancel = el("button", {
      type: "button",
      class: "button",
      text: t.t("action.cancel"),
      onclick: () => modal.close(),
    });
    const confirm = el("button", {
      type: "button",
      class: options.danger === false ? "button button-primary" : "button button-danger",
      text: options.confirmLabel ?? t.t("action.confirm"),
      onclick: () => {
        confirmed = true;
        modal.close();
      },
    });

    const modal = openModal({
      title: options.title ?? t.t("confirm.title"),
      closeLabel: t.t("action.close"),
      className: "modal-narrow",
      body: [
        el("p", { text: options.message }),
        options.detail ? el("p", { class: "muted", text: options.detail }) : null,
      ],
      actions: [cancel, confirm],
      onClose: () => resolve(confirmed),
    });

    // Cancel takes focus, not the confirming button: a stray Enter must not
    // delete an order.
    cancel.focus();
  });
}

/** Closes every open modal. Used when the router navigates away. */
export function closeAllModals(): void {
  while (stack.length > 0) {
    stack[stack.length - 1]?.close();
  }
}
