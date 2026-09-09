/**
 * Where to go back to after logging in.
 *
 * Somebody who presses "Log in" from an order is trying to join that order, not
 * to look at their account. The link carries where they were, and the login
 * form sends them back there.
 *
 * Registering is different and deliberately so: a new account has a display
 * name and an e-mail address to fill in, and the account page is where that
 * happens. So only the login half of the page honours this.
 */

/** The query parameter that carries it. */
export const RETURN_TO = "next";

/**
 * A link to the login page that remembers where it was pressed.
 *
 * `from` is normally the current location. It is passed in rather than read
 * here so that a page which builds its links before it is displayed -- every
 * page, as it happens -- records the address it belongs to rather than
 * whatever was on screen when the function ran.
 */
export function loginHref(from: string): string {
  if (!isSafeReturnPath(from) || from.startsWith("/account")) {
    // No point returning to the page somebody is already on, and a return to
    // /account is what happens anyway.
    return "/account";
  }
  return `/account?${RETURN_TO}=${encodeURIComponent(from)}`;
}

/**
 * Reads the return path, refusing anything that is not a path on this site.
 *
 * This is the open-redirect check, and it is the reason the value is validated
 * rather than trusted. A parameter that reaches `navigate()` unexamined lets
 * somebody send a link that logs a colleague in and then puts them on a page of
 * the sender's choosing -- which for a login page is the whole of the classic
 * attack. Anything that is not a single-slash-rooted path is discarded and the
 * account page is used instead.
 */
export function returnPathFrom(query: URLSearchParams): string | null {
  const raw = query.get(RETURN_TO);
  if (raw === null) {
    return null;
  }
  return isSafeReturnPath(raw) ? raw : null;
}

/**
 * Whether a value is a path on this site and nothing else.
 *
 * Rejected: an absolute URL, because it leaves the site; a protocol-relative
 * "//evil.example", because a browser reads that as one; a backslash, because
 * some browsers normalise "\\evil.example" the same way; and anything not
 * beginning with a slash, because a relative path resolves against wherever the
 * page happens to be.
 */
function isSafeReturnPath(value: string): boolean {
  return (
    value.startsWith("/") &&
    !value.startsWith("//") &&
    !value.startsWith("/\\") &&
    !value.includes("\\")
  );
}

/**
 * The address of the page being built, for a link that wants to come back.
 *
 * Read at the moment a link is built rather than when it is clicked, which is
 * the same thing for every page here: a page builds its links while it is being
 * rendered, so this is that page's own address.
 */
export function currentPath(): string {
  return location.pathname + location.search;
}
