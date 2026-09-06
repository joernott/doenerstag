// The three cookies the frontend sets for itself.
//
// docs/06_ui_ux.md lists them: the chosen language, the theme, and the last
// login name for prefilling the form. None is used for tracking, all three are
// readable by script -- unlike the session cookie, which is HttpOnly and set by
// the server -- and all three are set with the same attributes, which is why
// they go through one function rather than being written by hand at three call
// sites.

/** The interface language. */
export const LANGUAGE_COOKIE = "doener_lang";

/** `dark` or `light`. */
export const THEME_COOKIE = "doener_theme";

/** The last user name typed into the login form. */
export const NAME_COOKIE = "doener_name";

/** The CSRF token, set by the server and read back into a request header. */
export const CSRF_COOKIE = "doener_csrf";

/** A year, per docs/06_ui_ux.md. */
const oneYear = 365 * 24 * 60 * 60;

/** Reads a cookie, or null when it is not set. */
export function readCookie(name: string): string | null {
  for (const entry of document.cookie.split(";")) {
    const separator = entry.indexOf("=");
    if (separator < 0) {
      continue;
    }
    if (entry.slice(0, separator).trim() === name) {
      return decodeURIComponent(entry.slice(separator + 1).trim());
    }
  }
  return null;
}

/**
 * Writes one of our own cookies.
 *
 * `SameSite=Lax` rather than the session cookie's `Strict`: these carry no
 * authority, and a person following a link to an order from a chat window
 * should arrive in their own language rather than in English. `Secure` follows
 * the page, so `--no-https` in development still works.
 */
export function writeCookie(name: string, value: string): void {
  const parts = [
    `${name}=${encodeURIComponent(value)}`,
    "path=/",
    `max-age=${oneYear}`,
    "SameSite=Lax",
  ];
  if (location.protocol === "https:") {
    parts.push("Secure");
  }
  document.cookie = parts.join("; ");
}
