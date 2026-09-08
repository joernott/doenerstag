// Form controls.
//
// Every control is a real <input>, <select> or <textarea> with a real <label>
// bound to it by id, and every error is bound to its field by
// aria-describedby, which is what docs/06_ui_ux.md asks for and what makes the
// forms usable without a mouse or without sight.

import { append, el, type Child } from "../dom";

let sequence = 0;

/** A unique id, for binding a label to its control. */
export function fieldId(prefix = "field"): string {
  return `${prefix}-${++sequence}`;
}

export interface FieldOptions {
  /** The visible label. */
  label: string;
  /** The control. Its id is set if it has none. */
  control: HTMLElement;
  /** A hint below the control: a password rule, a format. */
  hint?: string;
  /** An error message. Announced, and bound to the control. */
  error?: string;
  /** Marks the field optional in the label, per the catalog's wording. */
  optionalLabel?: string;
}

/** A labelled control with its hint and error. */
export function field(options: FieldOptions): HTMLElement {
  const control = options.control;
  if (!control.id) {
    control.id = fieldId();
  }

  const described: string[] = [];
  const parts: Child[] = [];

  const label = el("label", { class: "field-label", for: control.id });
  append(label, options.label);
  if (options.optionalLabel) {
    label.appendChild(el("span", { class: "field-optional", text: ` (${options.optionalLabel})` }));
  }
  parts.push(label, control);

  if (options.hint) {
    const hintId = `${control.id}-hint`;
    described.push(hintId);
    parts.push(el("p", { class: "field-hint", id: hintId, text: options.hint }));
  }
  if (options.error) {
    const errorId = `${control.id}-error`;
    described.push(errorId);
    control.setAttribute("aria-invalid", "true");
    parts.push(
      el("p", { class: "field-error", id: errorId, role: "alert", text: options.error }),
    );
  }
  if (described.length > 0) {
    control.setAttribute("aria-describedby", described.join(" "));
  }

  return el("div", { class: "field" }, ...parts);
}

export interface InputOptions {
  type?: string;
  name?: string;
  value?: string;
  placeholder?: string;
  required?: boolean;
  autocomplete?: string;
  disabled?: boolean;
  min?: string;
  max?: string;
  step?: string;
  oninput?: (event: Event) => void;
}

/** A text-like input. */
export function input(options: InputOptions = {}): HTMLInputElement {
  return el("input", {
    class: "input",
    type: options.type ?? "text",
    ...(options.name ? { name: options.name } : {}),
    ...(options.value === undefined ? {} : { value: options.value }),
    ...(options.placeholder ? { placeholder: options.placeholder } : {}),
    ...(options.autocomplete ? { autocomplete: options.autocomplete } : {}),
    ...(options.min === undefined ? {} : { min: options.min }),
    ...(options.max === undefined ? {} : { max: options.max }),
    ...(options.step === undefined ? {} : { step: options.step }),
    required: options.required === true,
    disabled: options.disabled === true,
    ...(options.oninput ? { oninput: options.oninput } : {}),
  });
}

/** A multi-line input, for notes. */
export function textarea(options: InputOptions & { rows?: number } = {}): HTMLTextAreaElement {
  const element = el("textarea", {
    class: "input",
    rows: options.rows ?? 3,
    ...(options.name ? { name: options.name } : {}),
    ...(options.placeholder ? { placeholder: options.placeholder } : {}),
    required: options.required === true,
    disabled: options.disabled === true,
    ...(options.oninput ? { oninput: options.oninput } : {}),
  });
  if (options.value !== undefined) {
    element.value = options.value;
  }
  return element;
}

export interface Choice {
  value: string;
  label: string;
}

/** A select. */
export function select(
  choices: readonly Choice[],
  options: { name?: string; value?: string; disabled?: boolean; onchange?: (event: Event) => void } = {},
): HTMLSelectElement {
  const element = el("select", {
    class: "input",
    ...(options.name ? { name: options.name } : {}),
    disabled: options.disabled === true,
    ...(options.onchange ? { onchange: options.onchange } : {}),
  });
  for (const choice of choices) {
    element.appendChild(
      el("option", { value: choice.value, text: choice.label, selected: choice.value === options.value }),
    );
  }
  if (options.value !== undefined) {
    element.value = options.value;
  }
  return element;
}

/** A checkbox with its own label beside it. */
export function checkbox(
  label: string,
  options: { name?: string; checked?: boolean; disabled?: boolean; onchange?: (event: Event) => void } = {},
): HTMLElement {
  const box = el("input", {
    type: "checkbox",
    class: "checkbox",
    ...(options.name ? { name: options.name } : {}),
    checked: options.checked === true,
    disabled: options.disabled === true,
    ...(options.onchange ? { onchange: options.onchange } : {}),
  });
  return el("label", { class: "checkbox-row" }, box, el("span", { text: label }));
}

export type ButtonVariant = "default" | "primary" | "danger" | "quiet";

export interface ButtonOptions {
  label: string;
  variant?: ButtonVariant;
  type?: "button" | "submit";
  disabled?: boolean;
  /** Explains a disabled control, per the "never a mystery" rule in docs/06. */
  title?: string;
  onclick?: (event: Event) => void;
}

/** A button. */
export function button(options: ButtonOptions): HTMLButtonElement {
  const variant = options.variant ?? "default";
  return el("button", {
    class: variant === "default" ? "button" : `button button-${variant}`,
    type: options.type ?? "button",
    text: options.label,
    disabled: options.disabled === true,
    ...(options.title ? { title: options.title } : {}),
    ...(options.onclick ? { onclick: options.onclick } : {}),
  });
}

/**
 * A form.
 *
 * Submission is always intercepted, but the element is still a <form> with a
 * submit button: that is what makes Enter work in a text field, and it is the
 * shape assistive technology expects.
 */
export function form(onSubmit: () => void, ...children: Child[]): HTMLFormElement {
  const element = el("form", {
    class: "form",
    novalidate: true,
    onsubmit: (event: Event) => {
      event.preventDefault();
      onSubmit();
    },
  });
  append(element, ...children);
  return element;
}

/** A form-level error, announced when it appears. */
export function formError(message: string): HTMLElement {
  return el("p", { class: "form-error", role: "alert", text: message });
}
