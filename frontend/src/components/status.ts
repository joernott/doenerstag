// The line under a form or a section that says how the last thing went, and
// the helper that runs an action and reports through it.
//
// Components use both, so they live here rather than in the pages module: a
// component reporting an error should not have to import a page.

import { errorMessage } from "../api";
import { el } from "../dom";
import type { Translator } from "../i18n";

/**
 * A line that reports what just happened.
 *
 * One element that is updated rather than a message appended each time, so a
 * form saved five times does not grow five confirmations. `role="status"`
 * announces it without stealing focus, which is right for "Saved." and right
 * for an error the person is about to see anyway in the field it belongs to.
 */
export interface StatusLine {
  element: HTMLElement;
  say(message: string): void;
  fail(message: string): void;
  clear(): void;
}

export function statusLine(): StatusLine {
  const element = el("p", { class: "status", role: "status" });
  return {
    element,
    say(message: string): void {
      element.textContent = message;
      element.classList.remove("status-error");
    },
    fail(message: string): void {
      element.textContent = message;
      element.classList.add("status-error");
    },
    clear(): void {
      element.textContent = "";
      element.classList.remove("status-error");
    },
  };
}

/** What runAction reports through, and what it holds still while it works. */
export interface ActionContext {
  status: StatusLine;
  t: Translator;
  /** Disabled while the action runs, so a second press cannot send it twice. */
  busy?: HTMLButtonElement | null;
}

/**
 * Runs something a person asked for and reports how it went.
 *
 * The status line is cleared first, the busy control disabled for the
 * duration, and a failure reported on the status line in the person's
 * language. Written once, because the same dozen lines written out by hand
 * some thirty times would have the clearing, the disabling or the reporting
 * forgotten somewhere. Resolves to whether it worked, for a caller that does
 * something more on success -- closes a dialog, say.
 */
export async function runAction(
  context: ActionContext,
  action: () => Promise<unknown>,
): Promise<boolean> {
  const { status, t, busy } = context;
  status.clear();
  if (busy) {
    busy.disabled = true;
  }
  try {
    await action();
    return true;
  } catch (error) {
    status.fail(errorMessage(t, error));
    return false;
  } finally {
    if (busy) {
      busy.disabled = false;
    }
  }
}

