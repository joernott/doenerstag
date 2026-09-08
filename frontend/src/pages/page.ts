// The shape every page has.
//
// Separate from the route table so that a page module can use these without
// importing the module that imports it.

import { append, el, type Child } from "../dom";

/** A page: a heading and whatever follows it. Also sets the document title. */
export function page(title: string, ...content: Child[]): HTMLElement {
  const article = el("article", { class: "page-body" }, el("h1", { class: "page-title", text: title }));
  append(article, ...content);
  document.title = `${title} — doenerstag`;
  return article;
}

/** A titled card. The restaurant page is four of these stacked. */
export function section(title: string, ...content: Child[]): HTMLElement {
  const element = el("section", { class: "card" }, el("h2", { class: "card-title", text: title }));
  append(element, ...content);
  return element;
}

/** A row of controls, laid out left to right and wrapping on a narrow screen. */
export function actions(...content: Child[]): HTMLElement {
  const row = el("div", { class: "actions" });
  append(row, ...content);
  return row;
}

/**
 * A line that reports what just happened.
 *
 * One element that is updated rather than a message appended each time, so a
 * form saved five times does not grow five confirmations. `role="status"`
 * announces it without stealing focus, which is right for "Saved." and right
 * for an error the person is about to see anyway in the field it belongs to.
 */
export function statusLine(): {
  element: HTMLElement;
  say(message: string): void;
  fail(message: string): void;
  clear(): void;
} {
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
