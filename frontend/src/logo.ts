// The doenerstag mark, as a DOM node.
//
// One asset for both themes. `assets/doenerstag.svg` draws the frame and the
// calendar text in `currentColor`, so whatever renders it decides the colour by
// setting `color` -- the title bar takes the text colour, a tile placeholder
// takes the muted one, the watermark takes it at a few percent opacity. An
// `<img>` could not do that: it is a separate document and inherits nothing
// from the page around it, which is why the two variants in contrib/ had to
// exist as separate files in the first place.
//
// The markup is parsed once, put into the document once as a `<symbol>`, and
// referenced by every copy with `<use>`. Parsing goes through DOMParser rather
// than `innerHTML`, so the rule in docs/11_nonfunctional.md stands: this file
// does not introduce a second place that writes markup into the live document.
// What it parses is a build-time asset from this repository, never anything a
// user supplied.

import source from "../assets/doenerstag.svg";

/** The id of the symbol every copy of the mark points at. */
const SYMBOL_ID = "doenerstag-mark";

/** The parsed mark, or null if the asset ever fails to parse. */
const template: SVGSVGElement | null = parse();

/**
 * Puts the drawing into the document once, as a `<symbol>`.
 *
 * The first version of this cloned the whole drawing per use, which is fifty
 * seven elements each. The order overview draws one per tile, and at a hundred
 * and twenty five orders that was seven thousand SVG nodes on the start page:
 * enough that axe-core could no longer finish walking the document inside a
 * minute in Firefox. A symbol referenced by `<use>` costs two nodes per copy
 * instead, and `currentColor` still resolves against each `<use>`, so nothing
 * about the theming changes.
 *
 * Idempotent, and lazy: pages that never draw the mark never pay for it.
 */
function ensureSprite(): boolean {
  if (!template || typeof document === "undefined") {
    return false;
  }
  if (document.getElementById(SYMBOL_ID)) {
    return true;
  }

  const ns = "http://www.w3.org/2000/svg";
  const symbol = document.createElementNS(ns, "symbol");
  symbol.setAttribute("id", SYMBOL_ID);
  const viewBox = template.getAttribute("viewBox");
  if (viewBox) {
    symbol.setAttribute("viewBox", viewBox);
  }
  for (const child of Array.from(template.childNodes)) {
    symbol.appendChild(child.cloneNode(true));
  }

  // Hidden, but not with `display: none`: a symbol inside a hidden subtree is
  // still referenceable, while `hidden` on the sprite would be honoured by some
  // engines for the referencing copies too. Zero size and out of flow is the
  // safe way to keep it out of the layout, and that goes in the stylesheet
  // rather than in a `style` attribute -- the application's CSP is
  // `style-src 'self'` with no `unsafe-inline`, so an inline style is refused
  // by the browser and reported as a policy violation.
  const sprite = document.createElementNS(ns, "svg");
  sprite.setAttribute("aria-hidden", "true");
  sprite.setAttribute("class", "logo-sprite");
  sprite.appendChild(symbol);
  document.body.appendChild(sprite);
  return true;
}

function parse(): SVGSVGElement | null {
  // jsdom and a real browser both have DOMParser; a build without one would be
  // a broken build, but returning null keeps a missing mark from taking a page
  // down with it.
  if (typeof DOMParser === "undefined") {
    return null;
  }
  const parsed = new DOMParser().parseFromString(source, "image/svg+xml");
  const root = parsed.documentElement;
  // A parse failure yields a <parsererror> document rather than throwing.
  if (!(root instanceof SVGSVGElement)) {
    return null;
  }
  return root;
}

export interface LogoOptions {
  /** Added to the element's class list. */
  class?: string;
  /**
   * The accessible name.
   *
   * Omitted by default: the mark sits next to the application's name in the
   * title bar and beside a restaurant's name in a tile, so announcing it again
   * would be repetition. Pass a label only where the mark stands alone.
   */
  label?: string;
}

/**
 * A fresh copy of the mark.
 *
 * Returns a placeholder `<span>` rather than nothing when the asset could not
 * be parsed, so callers can append the result unconditionally.
 */
export function logoMark(options: LogoOptions = {}): Element {
  if (!ensureSprite() || !template) {
    return document.createElement("span");
  }

  const ns = "http://www.w3.org/2000/svg";
  const mark = document.createElementNS(ns, "svg");
  const viewBox = template.getAttribute("viewBox");
  if (viewBox) {
    mark.setAttribute("viewBox", viewBox);
  }
  if (options.class) {
    mark.setAttribute("class", options.class);
  }

  if (options.label === undefined) {
    mark.setAttribute("aria-hidden", "true");
  } else {
    mark.setAttribute("role", "img");
    const title = document.createElementNS(ns, "title");
    title.textContent = options.label;
    mark.appendChild(title);
  }

  const use = document.createElementNS(ns, "use");
  // The plain `href` attribute, not the deprecated `xlink:href`. Both are
  // same-document fragments, which is what keeps this working under a CSP that
  // allows nothing external.
  use.setAttribute("href", `#${SYMBOL_ID}`);
  mark.appendChild(use);
  return mark;
}
