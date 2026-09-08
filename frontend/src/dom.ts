// Element construction.
//
// Everything in this application builds its DOM through these helpers and never
// through innerHTML. That is not a style preference: the Content-Security-Policy
// in docs/05_auth_and_permissions.md forbids inline scripts, and user-entered
// text -- restaurant names, notes, display names -- reaches the page unescaped
// the moment somebody reaches for a template string. textContent cannot be
// talked into executing anything.

/** Anything that can be appended to an element. */
export type Child = Node | string | number | null | undefined | false;

/**
 * An attribute value.
 *
 * A key beginning with `on` whose value is a function becomes an event
 * listener; `class` and `text` are handled specially; `true` sets a bare
 * attribute and `false`, `null` and `undefined` omit it entirely, which is what
 * makes `{ disabled: !canEdit }` read the way it should.
 */
export type AttributeValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | EventListenerOrEventListenerObject;

/** Builds an element. */
export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attributes: Record<string, AttributeValue> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag);

  for (const [name, value] of Object.entries(attributes)) {
    if (value === null || value === undefined || value === false) {
      continue;
    }
    if (name.startsWith("on") && typeof value === "function") {
      element.addEventListener(name.slice(2).toLowerCase(), value);
      continue;
    }
    if (typeof value === "function" || typeof value === "object") {
      // A listener under a name that does not begin with `on`. Stringifying it
      // would put "[object Object]" in an attribute and look like it worked.
      continue;
    }
    if (name === "text") {
      element.textContent = String(value);
      continue;
    }
    element.setAttribute(name, value === true ? "" : String(value));
  }

  append(element, ...children);
  return element;
}

/** Appends children, skipping the empty ones so `cond && node` works inline. */
export function append(parent: Node, ...children: Child[]): void {
  for (const child of children) {
    if (child === null || child === undefined || child === false) {
      continue;
    }
    parent.appendChild(typeof child === "object" ? child : document.createTextNode(String(child)));
  }
}

/** Replaces an element's contents. */
export function replace(parent: Element, ...children: Child[]): void {
  parent.replaceChildren();
  append(parent, ...children);
}

/** A document fragment, for returning several nodes from one function. */
export function fragment(...children: Child[]): DocumentFragment {
  const result = document.createDocumentFragment();
  append(result, ...children);
  return result;
}

/**
 * The icons the chrome uses, as SVG path data.
 *
 * Inline SVG rather than an icon font or sprite sheet: five icons do not earn a
 * dependency, and a font would need a `font-src` the CSP does not grant.
 * Outlines from the Lucide set, which is ISC licensed.
 */
const iconPaths: Record<string, string[]> = {
  menu: ["M4 6h16", "M4 12h16", "M4 18h16"],
  close: ["M6 6l12 12", "M18 6L6 18"],
  sun: [
    "M12 4v2",
    "M12 18v2",
    "M4 12h2",
    "M18 12h2",
    "M6.3 6.3l1.4 1.4",
    "M16.3 16.3l1.4 1.4",
    "M17.7 6.3l-1.4 1.4",
    "M7.7 16.3l-1.4 1.4",
  ],
  moon: ["M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5z"],
  logout: ["M9 20H5V4h4", "M15 16l4-4-4-4", "M19 12H9"],
  plus: ["M12 5v14", "M5 12h14"],
  user: ["M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8z", "M4 21a8 8 0 0 1 16 0"],
};

/** The name of every icon, so a test can assert none is missing. */
export const iconNames = Object.keys(iconPaths);

/**
 * Builds an icon.
 *
 * Decorative by default -- `aria-hidden`, because the button around it carries
 * the accessible name. Passing a label makes the icon itself the label instead,
 * for the rare case where there is no text anywhere.
 */
export function icon(name: string, label?: string): SVGSVGElement {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "1.75");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  svg.setAttribute("class", "icon");
  if (label === undefined) {
    svg.setAttribute("aria-hidden", "true");
  } else {
    svg.setAttribute("role", "img");
    const title = document.createElementNS(ns, "title");
    title.textContent = label;
    svg.appendChild(title);
  }

  for (const data of iconPaths[name] ?? []) {
    const path = document.createElementNS(ns, "path");
    path.setAttribute("d", data);
    svg.appendChild(path);
  }
  return svg;
}

/** The focusable descendants of an element, in tab order. */
export function focusable(root: ParentNode): HTMLElement[] {
  const selector = [
    "a[href]",
    "button:not([disabled])",
    "input:not([disabled]):not([type=hidden])",
    "select:not([disabled])",
    "textarea:not([disabled])",
    "[tabindex]:not([tabindex='-1'])",
  ].join(",");

  // Hidden by attribute rather than hidden by layout: `offsetParent` would be
  // the thorough test, but it is a layout question, and the only things this
  // application hides are hidden with the `hidden` attribute or aria-hidden.
  // Asking about the attributes is also the only version that can be tested,
  // since a headless DOM has no layout to ask about.
  return [...root.querySelectorAll<HTMLElement>(selector)].filter(
    (element) =>
      !element.closest("[hidden]") &&
      element.getAttribute("aria-hidden") !== "true" &&
      element.closest("[inert]") === null,
  );
}
