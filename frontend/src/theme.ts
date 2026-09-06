// The dark and light themes.
//
// Dark is the default (docs/06_ui_ux.md). The choice lives in the doener_theme
// cookie and is applied as a `data-theme` attribute on the root element, which
// the stylesheet keys off. The same attribute is set by a tiny script in the
// document head before first paint, so a light-mode user never sees a dark
// flash; this module is what agrees with it afterwards.

import { readCookie, THEME_COOKIE, writeCookie } from "./cookies";

export type Theme = "dark" | "light";

/** The theme in force, defaulting to dark when nothing has been chosen. */
export function currentTheme(): Theme {
  return document.documentElement.dataset["theme"] === "light" ? "light" : "dark";
}

/** The theme the cookie asks for, or null when there is no cookie. */
export function storedTheme(): Theme | null {
  const value = readCookie(THEME_COOKIE);
  return value === "dark" || value === "light" ? value : null;
}

/**
 * Sets the theme without remembering it.
 *
 * For start-up: a visitor who has never chosen gets the dark default, and no
 * cookie is written until they actually choose something. A cookie set on
 * arrival would be a cookie nobody asked for.
 */
export function initTheme(theme: Theme | null): void {
  document.documentElement.dataset["theme"] = theme ?? "dark";
}

/** Applies a theme and remembers it. */
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset["theme"] = theme;
  writeCookie(THEME_COOKIE, theme);
}

/** Switches to the other theme and returns it. */
export function toggleTheme(): Theme {
  const next: Theme = currentTheme() === "dark" ? "light" : "dark";
  applyTheme(next);
  return next;
}
