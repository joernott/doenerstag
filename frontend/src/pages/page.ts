// The shape every page has.
//
// Separate from the route table so that a page module can use these without
// importing the module that imports it.

import { errorMessage } from "../api";
import { append, el, type Child } from "../dom";
import type { Translator } from "../i18n";
import { logoMark } from "../logo";

/** A page: a heading and whatever follows it. Also sets the document title. */
export function page(title: string, ...content: Child[]): HTMLElement {
  const article = el("article", { class: "page-body" }, el("h1", { class: "page-title", text: title }));
  append(article, ...content);
  document.title = `${title} — doenerstag`;
  return article;
}

/**
 * An overview page: a page with the mark behind it.
 *
 * The two grids -- orders and restaurants -- are the pages somebody lands on,
 * and a grid of cards on an empty background is a lot of nothing. The mark sits
 * centred behind them at a few percent opacity, scaled to most of the page's
 * height, and is `aria-hidden` and untouchable by the pointer: it is wallpaper,
 * not content, and nothing about the page changes if it fails to draw.
 */
export function overviewPage(title: string, ...content: Child[]): HTMLElement {
  const article = page(title, ...content);
  article.classList.add("page-watermarked");
  article.insertBefore(logoMark({ class: "page-watermark" }), article.firstChild);
  return article;
}

/**
 * A page whose heading carries controls.
 *
 * The restaurant page puts Save and Delete on the title's own line, aligned
 * with the right edge of the card below, so the two things that act on the
 * whole restaurant sit with its name rather than inside whichever tab happens
 * to be open.
 */
export function pageWithActions(
  title: string,
  controls: HTMLElement,
  ...content: Child[]
): HTMLElement {
  const heading = el("h1", { class: "page-title", text: title });
  const article = el(
    "article",
    { class: "page-body" },
    el("div", { class: "page-heading" }, heading, controls),
  );
  append(article, ...content);
  setPageTitle(article, title);
  return article;
}

/**
 * An overview whose heading carries controls.
 *
 * The watermark and the heading line, which the two functions above provide
 * separately. Composed rather than duplicated so that a change to either shows
 * up here without anybody remembering to make it twice.
 */
export function overviewPageWithActions(
  title: string,
  controls: HTMLElement,
  ...content: Child[]
): HTMLElement {
  const article = pageWithActions(title, controls, ...content);
  article.classList.add("page-watermarked");
  article.insertBefore(logoMark({ class: "page-watermark" }), article.firstChild);
  return article;
}

/**
 * Renames a page after it has been built.
 *
 * Changing only the browser tab would leave the heading above the form saying
 * the old name until the page was reloaded, which reads as a save that did
 * not take. The heading is found rather than passed back to
 * the caller so that any page gains this without changing its shape.
 */
export function setPageTitle(root: HTMLElement, title: string): void {
  const heading = root.querySelector(".page-title");
  if (heading) {
    heading.textContent = title;
  }
  document.title = `${title} — doenerstag`;
}

/** A titled card. */
export function section(title: string, ...content: Child[]): HTMLElement {
  const element = el("section", { class: "card" }, el("h2", { class: "card-title", text: title }));
  append(element, ...content);
  return element;
}

/**
 * A card with no heading of its own.
 *
 * What a tab panel wants: the tab already names it, and repeating that name as
 * a heading inside says the same word twice for no gain. The element is still a
 * `section`, and the tabs component labels it from its tab, so it keeps its
 * place in the document outline.
 */
export function card(...content: Child[]): HTMLElement {
  const element = el("section", { class: "card" });
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
 * A page that could not be loaded: its title, and what went wrong, announced.
 * Every page that fetches before it can draw anything ends this way when the
 * fetch fails, with this one line.
 */
export function failedPage(title: string, t: Translator, error: unknown): HTMLElement {
  return page(title, el("p", { class: "field-error", role: "alert", text: errorMessage(t, error) }));
}
