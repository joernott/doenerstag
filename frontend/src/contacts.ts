// What a contact turns into: a link, and an icon that says what kind.
//
// The restaurant page puts a button behind each contact's value and the order
// page renders the same contacts as links, so the rule for "what does this
// value open" lives here rather than in each of them.

/** The fields of a contact this module needs. */
export interface Linkable {
  contact_type_code: string;
  render_as: string;
  value: string;
}

/** Where a contact leads, and whether that is somewhere else. */
export interface ContactTarget {
  href: string;
  /** Opened in a new tab, with `rel="noreferrer"`. */
  external: boolean;
}

/**
 * The link a contact opens, or null when it does not open anything.
 *
 * `render_as` comes from the seeded contact types (migration 3), which is what
 * decides this rather than the code: `phone`, `mobile` and `fax` all render as
 * `tel`, and a contact type added to the catalog later inherits the behaviour
 * of whichever kind it declares itself to be.
 *
 * `text` -- which is what the "other" type is -- returns null. There is nothing
 * sensible to do with an arbitrary string, and a button that opens nothing is
 * worse than no button.
 */
export function contactTarget(contact: Linkable): ContactTarget | null {
  const value = contact.value.trim();
  if (value === "") {
    return null;
  }

  switch (contact.render_as) {
    case "tel":
      // Spaces and the punctuation people write phone numbers with are not
      // valid in a tel: URI; the digits, the leading + and the extension
      // separator are.
      return { href: `tel:${value.replace(/[^\d+;,*#]/g, "")}`, external: false };

    case "mailto":
      return { href: `mailto:${encodeURIComponent(value)}`, external: false };

    case "url":
      // A website typed without a scheme is still a website. Defaulting to
      // https rather than http: this application is served over TLS and a link
      // that silently downgrades is not what somebody meant.
      return {
        href: /^https?:\/\//i.test(value) ? value : `https://${value}`,
        external: true,
      };

    case "address":
      // The Maps URL API, which is the documented and stable way in: it takes a
      // free-text query and works without a key.
      return {
        href: `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(value)}`,
        external: true,
      };

    default:
      return null;
  }
}

/**
 * The icon for a contact type.
 *
 * Keyed on the code rather than on `render_as`, because a telephone, a mobile
 * and a fax all render as `tel` and are three different things to a person
 * looking at a list of them. An unknown code falls back to the shape of its
 * rendering, so a contact type added to the catalog gets something sensible
 * without a change here.
 */
export function contactIcon(contact: Linkable): string {
  switch (contact.contact_type_code) {
    case "phone":
      return "phone";
    case "mobile":
      return "mobile";
    case "fax":
      return "fax";
    case "email":
      return "mail";
    case "website":
      return "globe";
    case "address":
      return "map-pin";
    default:
      break;
  }

  switch (contact.render_as) {
    case "tel":
      return "phone";
    case "mailto":
      return "mail";
    case "url":
      return "globe";
    case "address":
      return "map-pin";
    default:
      return "";
  }
}
