// The translation runtime.

import { describe, expect, it } from "vitest";

import {
  hasLanguage,
  interpolate,
  languages,
  referenceName,
  resolveLanguage,
  tagName,
  Translator,
} from "../src/i18n";

describe("the language list", () => {
  it("comes from the catalogs in the build", () => {
    expect(languages().map((meta) => meta.code).sort()).toEqual(["de", "en"]);
  });

  it("names each language in its own language", () => {
    const german = languages().find((meta) => meta.code === "de");
    expect(german?.endonym).toBe("Deutsch");
  });

  it("knows which languages exist", () => {
    expect(hasLanguage("de")).toBe(true);
    expect(hasLanguage("fr")).toBe(false);
  });
});

describe("resolving the language", () => {
  it("prefers the cookie", () => {
    expect(resolveLanguage("de", ["en-GB", "en"])).toBe("de");
  });

  it("ignores a cookie naming a language this build does not have", () => {
    // The case docs/07_i18n.md calls out: an installation that once shipped a
    // language and no longer does must not produce an empty interface.
    expect(resolveLanguage("fr", ["de-AT"])).toBe("de");
  });

  it("matches the browser's preferences by primary subtag, in its order", () => {
    expect(resolveLanguage(null, ["de-CH", "en"])).toBe("de");
    expect(resolveLanguage(null, ["en-US", "de"])).toBe("en");
  });

  it("falls back to English", () => {
    expect(resolveLanguage(null, ["fr-CA", "it"])).toBe("en");
  });
});

describe("looking a message up", () => {
  it("translates", () => {
    expect(new Translator("de").t("action.save")).toBe("Speichern");
    expect(new Translator("en").t("action.save")).toBe("Save");
  });

  it("returns the key when nothing has it", () => {
    // Deliberately visible: a screen showing `order.nonexistent` is obviously
    // broken, where a blank is merely puzzling.
    expect(new Translator("de").t("order.nonexistent")).toBe("order.nonexistent");
  });

  it("falls back to a language it does have", () => {
    expect(new Translator("fr").language).toBe("en");
  });

  it("interpolates named placeholders", () => {
    expect(interpolate("{count} of {total}", { count: 2, total: 9 })).toBe("2 of 9");
  });

  it("leaves an unknown placeholder in place", () => {
    expect(interpolate("{count} of {total}", { count: 2 })).toBe("2 of {total}");
  });

  it("picks the plural category Intl asks for", () => {
    const en = new Translator("en");
    expect(en.t("order.items", { count: 1 })).toBe("1 item");
    expect(en.t("order.items", { count: 9 })).toBe("9 items");

    const de = new Translator("de");
    expect(de.t("order.items", { count: 1 })).toBe("1 Position");
    expect(de.t("order.items", { count: 9 })).toBe("9 Positionen");
  });

  it("reports whether a key exists", () => {
    const t = new Translator("en");
    expect(t.has("error.4001")).toBe(true);
    expect(t.has("error.7777")).toBe(false);
  });
});

describe("reference data", () => {
  it("translates a code", () => {
    expect(referenceName(new Translator("de"), "allergen", "gluten")).toBe(
      "Glutenhaltiges Getreide",
    );
  });

  it("falls back to the code itself", () => {
    expect(referenceName(new Translator("de"), "currency", "XTS")).toBe("XTS");
  });

  it("translates a seeded tag and keeps an invented one as typed", () => {
    const t = new Translator("de");
    expect(tagName(t, { code: "vegan", name: "vegan" })).toBe("Vegan");
    expect(tagName(t, { code: "extra_knoblauch", name: "extra Knoblauch" })).toBe(
      "extra Knoblauch",
    );
  });
});
