// The translation runtime.
//
// Catalogs are compiled into the bundle at build time; nothing is fetched at
// runtime. The set of languages is whatever catalogs the build contains, which
// is why every function here works from the registry rather than from a list of
// codes. See docs/07_i18n.md.

import { catalogs, type Catalog, type CatalogMeta } from "./registry.generated";

/** The reserved key each catalog uses to describe itself. */
export const META_KEY = "_meta";

/** The language every catalog is checked against, and the final fallback. */
export const SOURCE_LANGUAGE = "en";

export type { Catalog, CatalogMeta };

/**
 * Every language in this build, with its metadata.
 *
 * Derived from the catalogs, never from a hardcoded list: adding `fr.json` and
 * rebuilding is the whole of adding French.
 */
export function languages(): CatalogMeta[] {
  return Object.values(catalogs)
    .map((catalog) => catalog[META_KEY] as CatalogMeta)
    .sort((a, b) => a.endonym.localeCompare(b.endonym));
}

/** Whether a code names a language present in this build. */
export function hasLanguage(code: string): boolean {
  return Object.prototype.hasOwnProperty.call(catalogs, code);
}

/**
 * Resolves the interface language, per docs/07_i18n.md: the cookie, then the
 * browser's preferences, then English.
 *
 * Each step is checked against the registry, so a cookie left over from an
 * installation that once had a language falls through instead of producing an
 * empty interface.
 */
export function resolveLanguage(
  cookieValue: string | null,
  acceptLanguages: readonly string[],
): string {
  if (cookieValue && hasLanguage(cookieValue)) {
    return cookieValue;
  }

  for (const tag of acceptLanguages) {
    // Match on the primary subtag, so de-AT and de-CH both find de. The
    // browser hands them over in its own preference order, which is the order
    // to honour.
    const primary = tag.split("-")[0]?.toLowerCase();
    if (primary && hasLanguage(primary)) {
      return primary;
    }
  }

  return SOURCE_LANGUAGE;
}

/** A value that can be interpolated into a message. */
export type Placeholder = string | number;

/** The arguments to a lookup: named placeholders and an optional count. */
export interface TranslateOptions {
  [name: string]: Placeholder | undefined;
  /** Selects the plural category, via Intl.PluralRules. */
  count?: number;
}

/**
 * A translator bound to one language.
 *
 * Created once when the language is resolved and passed to whatever renders,
 * rather than reading a global: a component that takes its translator as an
 * argument can be tested in any language without touching global state.
 */
export class Translator {
  readonly language: string;
  readonly meta: CatalogMeta;

  private readonly catalog: Catalog;
  private readonly fallback: Catalog;
  private readonly plurals: Intl.PluralRules;

  constructor(language: string) {
    this.language = hasLanguage(language) ? language : SOURCE_LANGUAGE;
    this.catalog = catalogs[this.language] ?? {};
    this.fallback = catalogs[SOURCE_LANGUAGE] ?? {};
    this.meta = this.catalog[META_KEY] as CatalogMeta;
    this.plurals = new Intl.PluralRules(this.language);
  }

  /**
   * Looks a key up and interpolates its placeholders.
   *
   * Missing keys fall back to English and then to the key itself. Returning the
   * key rather than an empty string is deliberate: a screen showing
   * `order.deadline.label` is obviously broken, whereas one showing a blank is
   * merely puzzling, and the catalog completeness check exists so neither
   * happens in a shipped build.
   */
  t(key: string, options: TranslateOptions = {}): string {
    const template = this.lookup(key, options.count);
    if (template === undefined) {
      return key;
    }
    return interpolate(template, options);
  }

  /**
   * Whether a key exists in this catalog or the English one.
   *
   * For the callers that need to decide between a translation and a fallback --
   * an error code with no entry falls back to the API's English message rather
   * than to the key.
   */
  has(key: string): boolean {
    return this.lookup(key, undefined) !== undefined;
  }

  private lookup(key: string, count: number | undefined): string | undefined {
    if (count !== undefined) {
      // Ask Intl which category this language uses for this number, rather
      // than assuming one/other. A language with six categories supplies six
      // and gets the right one.
      const category = this.plurals.select(count);
      const plural =
        readString(this.catalog, `${key}.${category}`) ??
        readString(this.fallback, `${key}.${category}`) ??
        readString(this.fallback, `${key}.other`);
      if (plural !== undefined) {
        return plural;
      }
    }
    return readString(this.catalog, key) ?? readString(this.fallback, key);
  }
}

function readString(catalog: Catalog, key: string): string | undefined {
  const value = catalog[key];
  return typeof value === "string" ? value : undefined;
}

/**
 * Replaces `{name}` placeholders.
 *
 * An unknown placeholder is left as written rather than blanked, so a mismatch
 * between a catalog and its call site is visible in the interface instead of
 * silently swallowing part of the sentence.
 */
export function interpolate(template: string, values: TranslateOptions): string {
  return template.replace(/\{(\w+)\}/g, (whole, name: string) => {
    const value = values[name];
    return value === undefined ? whole : String(value);
  });
}

/**
 * Translates a reference-data code.
 *
 * A code with no catalog entry falls back to the code itself. That is legible
 * for a currency, where EUR is universally understood, and merely ugly for the
 * rest -- which is what the completeness check in CI is for.
 */
export function referenceName(
  t: Translator,
  prefix: "currency" | "allergen" | "additive" | "contact_type" | "tag",
  code: string,
): string {
  const key = `${prefix}.${code}`;
  return t.has(key) ? t.t(key) : code;
}

/**
 * Translates a tag, which is the one reference table users may add to.
 *
 * A seeded tag has a catalog entry and is translated from its code. A tag a
 * user invented has none -- no catalog can anticipate it -- so it carries the
 * name they typed, and that is what gets shown.
 */
export function tagName(t: Translator, tag: { code: string; name: string }): string {
  const key = `tag.${tag.code}`;
  return t.has(key) ? t.t(key) : tag.name || tag.code;
}
