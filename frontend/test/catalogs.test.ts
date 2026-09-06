// Catalog completeness, and the coverage of everything that is keyed by code.
//
// These are the checks docs/07_i18n.md promises: a catalog missing a key the
// source language has fails the build, and a reference code present in the seed
// migrations with no matching key in the English catalog fails it too. They run
// against the files on disk, not against the generated registry, so nothing
// here can be satisfied by an artefact that happens to be stale.

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import {
  checkCatalogs,
  classify,
  loadCatalogs,
  metaKey,
  sourceLanguage,
} from "../scripts/i18n.mjs";
import { catalogs as registry } from "../src/i18n/registry.generated";

const files = await loadCatalogs();

function source(): Record<string, string> {
  const found = files.find((catalog) => catalog.code === sourceLanguage);
  if (!found) {
    throw new Error(`${sourceLanguage}.json is missing`);
  }
  return found.data as Record<string, string>;
}

/**
 * Reads a file from the repository, which is two levels above frontend/test.
 *
 * Deliberately not `new URL(..., import.meta.url)`: the bundler treats that
 * shape as an asset reference and tries to resolve every file the interpolated
 * path could name, which fails on the first one outside the project root.
 */
const here = dirname(fileURLToPath(import.meta.url));

function repoFile(path: string): string {
  return readFileSync(join(here, "..", "..", path), "utf8");
}

/**
 * The codes seeded by a migration.
 *
 * The seeds are the definition of what has to be translatable, so the test
 * reads them rather than repeating them: a fifteenth allergen added to the
 * migration fails this test until it has a catalog entry, which is the whole
 * point of the check.
 */
function seededCodes(table: string): string[] {
  const sql = repoFile("migrations/000003_reference_data.up.sql");
  const statement = new RegExp(`INSERT INTO ${table} \\([^)]*\\) VALUES([\\s\\S]*?);`, "u");
  const block = statement.exec(sql);
  expect(block, `no INSERT INTO ${table} in the reference data migration`).not.toBeNull();

  // The code is the first single-quoted value that is not a UUID, which is how
  // every one of these tables is written: an optional id, then the code.
  const codes: string[] = [];
  for (const row of block?.[1]?.split("\n") ?? []) {
    const values = [...row.matchAll(/'([^']*)'/gu)].map((match) => match[1] ?? "");
    const code = values.find((value) => !/^[0-9a-f-]{36}$/u.test(value));
    if (code !== undefined && values.length > 0) {
      codes.push(code);
    }
  }
  return codes;
}

describe("the catalogs", () => {
  it("ships at least the two documented languages", () => {
    expect(files.map((catalog) => catalog.code).sort()).toEqual(
      expect.arrayContaining(["de", "en"]),
    );
  });

  it("is complete in every language", () => {
    const { errors } = checkCatalogs(files);
    expect(errors).toEqual([]);
  });

  it("has no keys a translation invented", () => {
    const { warnings } = checkCatalogs(files);
    expect(warnings).toEqual([]);
  });

  it("describes itself in its own language", () => {
    for (const catalog of files) {
      const meta = catalog.data[metaKey] as { code: string; endonym: string; dir: string };
      expect(meta.code).toBe(catalog.code);
      expect(meta.endonym.length).toBeGreaterThan(0);
      expect(["ltr", "rtl"]).toContain(meta.dir);
    }
  });

  it("supplies every plural category its own language has", () => {
    const shape = classify(source(), sourceLanguage);
    expect([...shape.plurals].sort()).toEqual([
      "confirm.delete_order.participants",
      "order.items",
      "order.participants",
    ]);
  });
});

describe("the registry", () => {
  it("lists exactly the catalogs in the directory", () => {
    expect(Object.keys(registry).sort()).toEqual(files.map((catalog) => catalog.code).sort());
  });

  it("carries each catalog's own metadata", () => {
    for (const catalog of files) {
      expect(registry[catalog.code]?.[metaKey]).toEqual(catalog.data[metaKey]);
    }
  });
});

describe("reference data", () => {
  const tables: [string, string][] = [
    ["currency", "currency"],
    ["contact_type", "contact_type"],
    ["tag", "tag"],
    ["allergen", "allergen"],
    ["additive", "additive"],
  ];

  for (const [table, prefix] of tables) {
    it(`has a name for every seeded ${table}`, () => {
      const codes = seededCodes(table);
      expect(codes.length).toBeGreaterThan(0);
      for (const code of codes) {
        expect(Object.keys(source())).toContain(`${prefix}.${code}`);
      }
    });
  }

  it("has a message for every documented error number", () => {
    const api = repoFile("docs/04_api.md");
    // The column padding in a Markdown table is cosmetic and changes whenever
    // a wider row is added, so the separators are matched loosely.
    const rows = api.matchAll(/^\|\s*(\d{4})\s*\|\s*\d{3}\s*\|/gmu);
    const codes = [...rows].map((match) => match[1]);
    expect(codes.length).toBeGreaterThan(20);
    for (const code of codes) {
      expect(Object.keys(source())).toContain(`error.${code}`);
    }
  });
});
