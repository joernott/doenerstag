// The password field, with the live complexity indicator.
//
// docs/05_auth_and_permissions.md: "The frontend shows which rules are
// currently satisfied as the password is typed." The list is not a warning and
// not an error -- nothing is wrong yet -- so it is a plain list that fills in,
// announced politely rather than as an alert.

import { el } from "../dom";
import { field, input } from "./forms";
import type { Translator } from "../i18n";
import { CLASSES, checkPassword, MIN_LENGTH, REQUIRED_CLASSES } from "../password";

export interface PasswordFieldOptions {
  t: Translator;
  /** The visible label. Defaults to "Password". */
  label?: string;
  name?: string;
  /** `new-password` for registration and a change, `current-password` to log in. */
  autocomplete?: string;
  /** Whether to show the rules. Off for a login form: it is too late to advise. */
  indicator?: boolean;
}

export interface PasswordField {
  /** The whole control, label and indicator included. */
  element: HTMLElement;
  /** The input, for reading the value and for focusing it after an error. */
  control: HTMLInputElement;
  /** What the server will decide about the current value. */
  acceptable(): boolean;
  value(): string;
}

/** Builds a password field. */
export function passwordField(options: PasswordFieldOptions): PasswordField {
  const { t } = options;

  const control = input({
    type: "password",
    name: options.name ?? "password",
    autocomplete: options.autocomplete ?? "new-password",
    required: true,
  });

  if (!options.indicator) {
    return {
      element: field({ label: options.label ?? t.t("auth.password"), control }),
      control,
      acceptable: () => control.value.length > 0,
      value: () => control.value,
    };
  }

  const rules = el("ul", { class: "rules" });
  const entries = new Map<string, { item: HTMLElement; mark: HTMLElement }>();

  const add = (key: string, text: string): void => {
    const mark = el("span", { class: "rule-mark", "aria-hidden": "true", text: "·" });
    const item = el("li", { class: "rule" }, mark, el("span", { text }));
    entries.set(key, { item, mark });
    rules.appendChild(item);
  };

  add("length", t.t("password.length", { count: MIN_LENGTH }));
  for (const name of CLASSES) {
    add(name, t.t(`password.${name}`));
  }

  // The list is a live region so that somebody using a screen reader hears the
  // rule they have just satisfied, rather than discovering at submit time that
  // they had not.
  rules.setAttribute("aria-live", "polite");

  const update = (): void => {
    const verdict = checkPassword(control.value);
    const met = new Set<string>(verdict.satisfied);
    if (verdict.longEnough) {
      met.add("length");
    }

    for (const [key, entry] of entries) {
      const satisfied = met.has(key);
      entry.item.classList.toggle("rule-met", satisfied);
      entry.mark.textContent = satisfied ? "✓" : "·";
    }
  };

  control.addEventListener("input", update);
  update();

  const element = field({
    label: options.label ?? t.t("auth.password"),
    control,
    hint: t.t("password.requirements", { count: REQUIRED_CLASSES }),
  });
  element.appendChild(rules);

  return {
    element,
    control,
    acceptable: () => checkPassword(control.value).acceptable,
    value: () => control.value,
  };
}
