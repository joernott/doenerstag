// The shell: the title bar, the language selector, the theme toggle and the
// main menu.
//
// This is where the sprint's exit criterion is checked -- the selector is
// populated from the catalogs in the build rather than from a list -- so the
// assertions compare it against the registry rather than against "English" and
// "Deutsch".

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createApp, type App } from "../src/app";
import { languages } from "../src/i18n";
import type { Route } from "../src/router";

function routes(app: App): Route[] {
  return [
    {
      pattern: "/",
      render: () => {
        const page = document.createElement("h1");
        page.textContent = app.t.t("nav.orders");
        return page;
      },
    },
  ];
}

function notFound(): Node {
  return document.createElement("p");
}

function build(language: string | null, theme: "dark" | "light" | null = null): App {
  const root = document.createElement("div");
  root.id = "app";
  document.body.appendChild(root);

  const app = createApp(root, { language, theme }, ["en-GB"], routes, notFound);
  app.render();
  return app;
}

function titlebar(): HTMLElement {
  const bar = document.querySelector<HTMLElement>(".titlebar");
  if (!bar) {
    throw new Error("no title bar");
  }
  return bar;
}

function menuLabels(): string[] {
  return [...document.querySelectorAll("#main-menu .menu-entry")].map((entry) => entry.textContent ?? "");
}

beforeEach(() => {
  document.body.replaceChildren();
  document.documentElement.removeAttribute("data-theme");
  for (const name of ["doener_lang", "doener_theme"]) {
    document.cookie = `${name}=; max-age=0; path=/`;
  }
});

afterEach(() => {
  document.body.replaceChildren();
});

describe("the language selector", () => {
  function languageEntries(): HTMLButtonElement[] {
    return [...titlebar().querySelectorAll<HTMLButtonElement>("#language-menu .menu-entry")];
  }

  it("is populated from the catalogs in the build", () => {
    build(null);
    const entries = languageEntries();

    expect(entries.map((entry) => entry.lang)).toEqual(languages().map((meta) => meta.code));
    // Each language by its own name: somebody looking for German looks for
    // "Deutsch".
    expect(entries.map((entry) => entry.textContent)).toEqual(
      languages().map((meta) => meta.endonym),
    );
  });

  it("shows the language in force on the button", () => {
    build("de");
    const button = titlebar().querySelector<HTMLButtonElement>("[aria-controls='language-menu']");
    expect(button?.textContent).toBe("Deutsch");
    // And says so to a screen reader in the list, where it is not marked visibly.
    const current = languageEntries().find((entry) => entry.getAttribute("aria-current") === "true");
    expect(current?.lang).toBe("de");
  });

  // 19.6: a list the page draws, because Firefox lets a page style nothing of
  // a native select's open list. It opens and closes like the main menu.
  it("opens from its button and closes on Escape", () => {
    build("en");
    const button = titlebar().querySelector<HTMLButtonElement>("[aria-controls='language-menu']");
    const list = document.getElementById("language-menu");
    if (!button || !list) {
      throw new Error("no language selector");
    }

    expect(list.hidden).toBe(true);
    button.click();
    expect(list.hidden).toBe(false);
    expect(button.getAttribute("aria-expanded")).toBe("true");

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(list.hidden).toBe(true);
    expect(button.getAttribute("aria-expanded")).toBe("false");
  });

  it("switches language without reloading, and remembers the choice", () => {
    const app = build("en");
    const german = languageEntries().find((entry) => entry.lang === "de");
    if (!german) {
      throw new Error("no language selector");
    }

    german.click();

    expect(app.language).toBe("de");
    expect(document.cookie).toContain("doener_lang=de");
    expect(menuLabels()).toContain("Bestellungen");
    expect(document.documentElement.lang).toBe("de");
    expect(document.documentElement.dir).toBe("ltr");
  });
});

describe("the theme", () => {
  it("starts dark and writes no cookie until somebody chooses", () => {
    build(null);
    expect(document.documentElement.dataset["theme"]).toBe("dark");
    expect(document.cookie).not.toContain("doener_theme");
  });

  it("follows the cookie", () => {
    build(null, "light");
    expect(document.documentElement.dataset["theme"]).toBe("light");
  });

  it("switches and remembers", () => {
    const app = build(null);
    const label = app.t.t("theme.toggle");
    const toggle = titlebar().querySelector<HTMLButtonElement>(`[aria-label="${label}"]`);
    toggle?.click();

    expect(app.theme).toBe("light");
    expect(document.documentElement.dataset["theme"]).toBe("light");
    expect(document.cookie).toContain("doener_theme=light");
  });
});

describe("the main menu", () => {
  it("shows an anonymous visitor only what they can use", () => {
    const app = build("en");
    expect(menuLabels()).toEqual(
      expect.arrayContaining([
        app.t.t("nav.orders"),
        app.t.t("nav.restaurants"),
        app.t.t("nav.version"),
      ]),
    );
    expect(menuLabels()).not.toContain(app.t.t("nav.account"));
    expect(menuLabels()).not.toContain(app.t.t("nav.users"));
  });

  it("adds the account entry once somebody is logged in", () => {
    const app = build("en");
    app.session.set({ id: "1", name: "jo", display_name: "Jo", is_admin: false });
    app.render();

    expect(menuLabels()).toContain(app.t.t("nav.account"));
    expect(menuLabels()).not.toContain(app.t.t("nav.users"));
  });

  it("adds the user administration for the administrator", () => {
    const app = build("en");
    app.session.set({ id: "1", name: "root", display_name: "Root", is_admin: true });
    app.render();

    expect(menuLabels()).toContain(app.t.t("nav.users"));
  });

  it("hides the API documentation when the server does not serve it", () => {
    const app = build("en");
    app.version = {
      version: "1.0.0",
      commit: "abc",
      build_date: "",
      swagger: false,
      max_image_size: 5242880,
    };
    app.render();

    expect(menuLabels()).not.toContain(app.t.t("nav.api_docs"));
  });

  it("opens and closes, and says so to a screen reader", () => {
    build("en");
    const button = titlebar().querySelector<HTMLButtonElement>("[aria-controls='main-menu']");
    const menu = document.getElementById("main-menu");
    if (!button || !menu) {
      throw new Error("no menu");
    }

    expect(button.getAttribute("aria-expanded")).toBe("false");
    expect(menu.hidden).toBe(true);

    button.click();
    expect(button.getAttribute("aria-expanded")).toBe("true");
    expect(menu.hidden).toBe(false);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(menu.hidden).toBe(true);
    expect(document.activeElement).toBe(button);
  });
});

describe("the account control", () => {
  it("offers login to an anonymous visitor", () => {
    const app = build("en");
    expect(titlebar().textContent).toContain(app.t.t("auth.login_or_register"));
  });

  it("shows the display name once logged in", () => {
    const app = build("en");
    app.session.set({ id: "1", name: "jo", display_name: "Jo Ott", is_admin: false });
    app.render();

    expect(titlebar().textContent).toContain("Jo Ott");
    expect(titlebar().textContent).not.toContain(app.t.t("auth.login_or_register"));
  });
});

describe("the shell", () => {
  it("puts a skip link first", () => {
    build("en");
    const first = document.querySelector(".shell")?.firstElementChild;
    expect(first?.classList.contains("skip-link")).toBe(true);
    expect(first?.getAttribute("href")).toBe("#main");
  });

  it("has a polite live region for changes nobody asked for", () => {
    const app = build("en");
    app.announce("an item was added");
    const region = document.getElementById("live-region");
    expect(region?.getAttribute("aria-live")).toBe("polite");
    expect(region?.textContent).toBe("an item was added");
  });
});
