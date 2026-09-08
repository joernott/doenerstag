// The catalog registry: the build-time half of the i18n system.
//
// docs/07_i18n.md says the set of languages is whatever catalogs the build
// contains and that no list of languages exists anywhere else. This file is
// what makes that true: it globs src/i18n/*.json and writes the registry module
// the runtime imports, so adding fr.json and rebuilding is the whole of adding
// French.
//
// It is also where the completeness check lives. Both the build and the test
// suite call the same functions, so what CI enforces and what `npm run build`
// enforces cannot drift apart.

import { readdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

/** Where the catalogs live. */
export const catalogDir = join(here, "..", "src", "i18n");

/** The module this script writes, imported by src/i18n/index.ts. */
export const registryFile = join(catalogDir, "registry.generated.ts");

/** The reserved key each catalog describes itself with. */
export const metaKey = "_meta";

/** The source language: complete by definition, and the fallback for the rest. */
export const sourceLanguage = "en";

/** The ECMA-402 plural categories, in the order CLDR lists them. */
const pluralCategories = ["zero", "one", "two", "few", "many", "other"];

/**
 * Reads every catalog, sorted by code so the generated file is byte-stable.
 *
 * Stability matters more than it looks: static/ is committed and CI fails when
 * a rebuild changes it, so a generator whose output depended on directory order
 * would produce spurious failures.
 */
export async function loadCatalogs() {
  const names = (await readdir(catalogDir))
    .filter((name) => name.endsWith(".json"))
    .sort();

  const catalogs = [];
  for (const name of names) {
    const file = join(catalogDir, name);
    catalogs.push({
      code: name.slice(0, -".json".length),
      file,
      name,
      data: JSON.parse(await readFile(file, "utf8")),
    });
  }
  return catalogs;
}

/** The plural categories the given language actually has. */
export function categoriesFor(code) {
  return new Intl.PluralRules(code).resolvedOptions().pluralCategories;
}

/**
 * Splits a catalog into ordinary keys and plural groups.
 *
 * A base key counts as a plural group only when the catalog supplies *every*
 * category of its own language for it. That rule exists because `other` is both
 * a plural category and an ordinary word: `contact_type.other` is a contact
 * type, not the plural of `contact_type`, and the only thing that distinguishes
 * them is that no `contact_type.one` accompanies it.
 */
export function classify(data, code) {
  const keys = Object.keys(data).filter((key) => key !== metaKey);
  const categories = categoriesFor(code);
  const candidates = new Map();

  for (const key of keys) {
    const cut = key.lastIndexOf(".");
    if (cut < 0) {
      continue;
    }
    const suffix = key.slice(cut + 1);
    if (!pluralCategories.includes(suffix)) {
      continue;
    }
    const base = key.slice(0, cut);
    const found = candidates.get(base) ?? new Set();
    found.add(suffix);
    candidates.set(base, found);
  }

  const plurals = new Set();
  for (const [base, found] of candidates) {
    if (categories.every((category) => found.has(category))) {
      plurals.add(base);
    }
  }

  const singles = keys.filter((key) => {
    const cut = key.lastIndexOf(".");
    return cut < 0 || !plurals.has(key.slice(0, cut));
  });

  return { singles: new Set(singles), plurals };
}

/** The placeholder names a message interpolates. */
export function placeholders(message) {
  return new Set([...message.matchAll(/\{(\w+)\}/g)].map((match) => match[1]));
}

/**
 * Checks the catalogs against each other and against their own metadata.
 *
 * Errors fail the build. Warnings do not: an extra key in a translation is
 * untidy, not broken, and a build that refused it would make removing a key
 * from the source language a two-commit operation.
 */
export function checkCatalogs(catalogs) {
  const errors = [];
  const warnings = [];

  for (const catalog of catalogs) {
    errors.push(...checkMeta(catalog));
  }

  const source = catalogs.find((catalog) => catalog.code === sourceLanguage);
  if (!source) {
    errors.push(`the source language ${sourceLanguage}.json is missing`);
    return { errors, warnings };
  }

  const shape = classify(source.data, source.code);

  for (const catalog of catalogs) {
    if (catalog.code === sourceLanguage) {
      continue;
    }
    const categories = categoriesFor(catalog.code);
    const required = new Set(shape.singles);
    for (const base of shape.plurals) {
      for (const category of categories) {
        required.add(`${base}.${category}`);
      }
    }

    for (const key of [...required].sort()) {
      if (typeof catalog.data[key] !== "string") {
        errors.push(`${catalog.name}: missing key ${key}`);
      }
    }

    for (const key of Object.keys(catalog.data)) {
      if (key !== metaKey && !required.has(key)) {
        warnings.push(`${catalog.name}: key ${key} is not in ${sourceLanguage}.json`);
      }
      const translated = catalog.data[key];
      const original = source.data[key];
      if (typeof translated !== "string" || typeof original !== "string") {
        continue;
      }
      const known = placeholders(original);
      for (const name of placeholders(translated)) {
        if (!known.has(name)) {
          warnings.push(
            `${catalog.name}: ${key} interpolates ${name}, which ${sourceLanguage}.json does not`,
          );
        }
      }
    }
  }

  return { errors, warnings };
}

function checkMeta(catalog) {
  const errors = [];
  const meta = catalog.data[metaKey];

  if (!meta || typeof meta !== "object") {
    return [`${catalog.name}: no ${metaKey} block`];
  }
  if (meta.code !== catalog.code) {
    errors.push(`${catalog.name}: ${metaKey}.code is "${meta.code}", expected "${catalog.code}"`);
  }
  if (typeof meta.endonym !== "string" || meta.endonym.length === 0) {
    errors.push(`${catalog.name}: ${metaKey}.endonym must name the language in its own language`);
  }
  if (meta.dir !== "ltr" && meta.dir !== "rtl") {
    errors.push(`${catalog.name}: ${metaKey}.dir is "${meta.dir}", expected "ltr" or "rtl"`);
  }

  for (const [key, value] of Object.entries(catalog.data)) {
    if (key !== metaKey && typeof value !== "string") {
      errors.push(`${catalog.name}: ${key} is not a string`);
    }
  }

  return errors;
}

/** Renders the registry module. */
export function registrySource(catalogs) {
  const imports = catalogs
    .map((catalog) => `import ${identifier(catalog.code)} from "./${catalog.name}";`)
    .join("\n");
  const entries = catalogs
    .map((catalog) => `  "${catalog.code}": ${identifier(catalog.code)},`)
    .join("\n");

  return `// Generated by scripts/i18n.mjs from the catalogs in this directory.
// Do not edit and do not commit it: every build writes it again.

${imports}

/** What a catalog says about itself in its reserved "${metaKey}" key. */
export interface CatalogMeta {
  /** The language tag, which is also the file name. */
  readonly code: string;
  /** The language's own name for itself, e.g. "Deutsch". */
  readonly endonym: string;
  /** "ltr" or "rtl". The build check rejects anything else. */
  readonly dir: string;
}

/** One catalog: the metadata block plus flat, dotted message keys. */
export type Catalog = Record<string, string | CatalogMeta>;

/** Every catalog in this build, keyed by language code. */
export const catalogs: Record<string, Catalog> = {
${entries}
};
`;
}

/** A language code as a JavaScript identifier: fr-CA becomes frCA. */
function identifier(code) {
  return code.replace(/[^A-Za-z0-9]+(.)?/g, (_, next) => (next ? next.toUpperCase() : ""));
}

/**
 * Writes the registry and reports on the catalogs.
 *
 * Returns the catalogs it found, so a caller can say what it built.
 */
export async function generateRegistry({ strict = true } = {}) {
  const catalogs = await loadCatalogs();
  const { errors, warnings } = checkCatalogs(catalogs);

  for (const warning of warnings) {
    console.warn(`i18n: ${warning}`);
  }
  if (errors.length > 0) {
    const listed = errors.map((error) => `  ${error}`).join("\n");
    const problem = new Error(`the catalogs are incomplete:\n${listed}`);
    if (strict) {
      throw problem;
    }
    console.error(problem.message);
  }

  await writeFile(registryFile, registrySource(catalogs), "utf8");
  return catalogs;
}

// Run directly -- `node scripts/i18n.mjs` -- so type-checking and the test
// suite can generate the registry without going through the whole build.
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  generateRegistry()
    .then((catalogs) => {
      const codes = catalogs.map((catalog) => catalog.code).join(", ");
      console.log(`i18n: ${catalogs.length} catalogs (${codes})`);
    })
    .catch((error) => {
      console.error(error.message);
      process.exit(1);
    });
}
