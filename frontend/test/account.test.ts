// The account page, in both of its shapes.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { accountPage } from "../src/pages/account";
import type { App } from "../src/app";
import { fails, mountApp, settle, stubServer } from "./helpers";

function render(app: App): HTMLElement {
  const rendered = accountPage(app);
  app.pageOutlet?.replaceChildren(rendered);
  return rendered;
}

function labelled(root: ParentNode, label: string): HTMLElement | null {
  for (const element of root.querySelectorAll("label")) {
    if (element.textContent?.startsWith(label)) {
      const id = element.getAttribute("for");
      return id ? root.querySelector<HTMLElement>(`#${id}`) : null;
    }
  }
  return null;
}

beforeEach(() => {
  document.body.replaceChildren();
  document.cookie = "doener_name=; max-age=0; path=/";
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("logged out", () => {
  it("offers login and register as two tabs, login first", () => {
    stubServer({});
    const app = mountApp(() => []);
    const rendered = render(app);

    const tabs = [...rendered.querySelectorAll<HTMLButtonElement>("[role='tab']")];
    expect(tabs.map((entry) => entry.textContent)).toEqual([
      app.t.t("auth.login"),
      app.t.t("auth.register"),
    ]);
    expect(tabs[0]?.getAttribute("aria-selected")).toBe("true");

    const panels = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")];
    expect(panels[0]?.hidden).toBe(false);
    expect(panels[1]?.hidden).toBe(true);
  });

  it("switches tab with the arrow keys", () => {
    stubServer({});
    const app = mountApp(() => []);
    const rendered = render(app);

    const tabs = [...rendered.querySelectorAll<HTMLButtonElement>("[role='tab']")];
    tabs[0]?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }));

    expect(tabs[1]?.getAttribute("aria-selected")).toBe("true");
    const panels = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")];
    expect(panels[1]?.hidden).toBe(false);
  });

  it("prefills the last user name from the cookie", () => {
    document.cookie = "doener_name=jo; path=/";
    stubServer({});
    const app = mountApp(() => []);
    const rendered = render(app);

    const name = labelled(rendered, app.t.t("auth.name")) as HTMLInputElement | null;
    expect(name?.value).toBe("jo");
  });

  it("shows the complexity rules on the register form and not on the login form", () => {
    stubServer({});
    const app = mountApp(() => []);
    const rendered = render(app);

    const panels = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")];
    expect(panels[0]?.querySelector(".rules")).toBeNull();
    expect(panels[1]?.querySelectorAll(".rule").length).toBe(5);
  });

  it("ticks a rule as it is satisfied", () => {
    stubServer({});
    const app = mountApp(() => []);
    const rendered = render(app);

    const panels = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")];
    const password = panels[1]?.querySelector<HTMLInputElement>("input[type='password']");
    if (!password) {
      throw new Error("no password field");
    }

    expect(panels[1]?.querySelectorAll(".rule-met").length).toBe(0);

    password.value = "Abcdefghij1!";
    password.dispatchEvent(new Event("input"));

    // Upper, lower, digit and special: four of the five lines. The length is
    // not one of them any more, it is the counter beside the field.
    expect(panels[1]?.querySelectorAll(".rule-met").length).toBe(4);

    const counter = panels[1]?.querySelector(".password-count");
    expect(counter?.textContent).toBe("12/10");
    expect(counter?.classList.contains("password-count-met")).toBe(true);
  });

  it("counts the characters typed, and only turns the counter over at ten", () => {
    stubServer({});
    const app = mountApp(() => []);
    const rendered = render(app);

    const panels = [...rendered.querySelectorAll<HTMLElement>("[role='tabpanel']")];
    const password = panels[1]?.querySelector<HTMLInputElement>("input[type='password']");
    const counter = panels[1]?.querySelector(".password-count");
    if (!password || !counter) {
      throw new Error("no password field");
    }

    expect(counter.textContent).toBe("0/10");

    password.value = "Abcdefghi";
    password.dispatchEvent(new Event("input"));
    expect(counter.textContent).toBe("9/10");
    expect(counter.classList.contains("password-count-met")).toBe(false);

    password.value = "Abcdefghij";
    password.dispatchEvent(new Event("input"));
    expect(counter.textContent).toBe("10/10");
    expect(counter.classList.contains("password-count-met")).toBe(true);

    // Code points, the same unit the rule counts: a decomposed "a" with a
    // combining acute is one character here and one character to the server.
    password.value = "Abcdefghi" + "á";
    password.dispatchEvent(new Event("input"));
    expect(counter.textContent).toBe("10/10");
  });
  it("logs in and rebuilds the chrome around the new session", async () => {
    const user = { id: "u1", name: "jo", display_name: "Jo", is_admin: false };
    stubServer({ "POST /auth/login": { user } });

    const app = mountApp(() => []);
    const rendered = render(app);

    const name = labelled(rendered, app.t.t("auth.name")) as HTMLInputElement | null;
    if (!name) {
      throw new Error("no user name field");
    }
    name.value = "jo";

    rendered.querySelector("form")?.dispatchEvent(new Event("submit"));
    await settle();

    expect(app.session.user).toEqual(user);
    expect(document.querySelector(".titlebar")?.textContent).toContain("Jo");
    expect(document.cookie).toContain("doener_name=jo");
  });

  it("says what went wrong when the password is refused", async () => {
    stubServer({
      "POST /auth/login": fails(401, {
        error: { code: 2001, message: "invalid user name or password" },
      }),
    });

    const app = mountApp(() => []);
    const rendered = render(app);
    rendered.querySelector("form")?.dispatchEvent(new Event("submit"));
    await settle();

    expect(rendered.querySelector(".status")?.textContent).toBe(app.t.t("error.2001"));
  });
});

describe("logged in", () => {
  const user = { id: "u1", name: "jo", display_name: "Jo Ott", is_admin: false };

  function loggedInApp(): App {
    const app = mountApp(() => []);
    app.session.set(user);
    return app;
  }

  it("has the profile, password and token tabs, and deletion on the heading", () => {
    stubServer({
      "GET /users/u1": { id: "u1", name: "jo", email: "jo@example.org" },
      "GET /users/u1/tokens": { tokens: [] },
    });

    const app = loggedInApp();
    const rendered = render(app);

    const tabs = [...rendered.querySelectorAll("[role='tab']")].map((entry) => entry.textContent);
    expect(tabs).toEqual([
      app.t.t("account.profile"),
      app.t.t("account.password"),
      app.t.t("account.tokens"),
    ]);

    // Deleting the account is not a section any more: it is a button on the
    // title's line, and the warning it used to print permanently is in the
    // dialog it opens, where it applies.
    const heading = rendered.querySelector(".page-heading-actions");
    const labels = [...(heading?.querySelectorAll("button") ?? [])].map(
      (control) => control.textContent,
    );
    expect(labels).toEqual([app.t.t("action.save"), app.t.t("account.delete")]);
    expect(rendered.textContent).not.toContain(app.t.t("account.delete.intro"));
  });

  it("keeps Save quiet until a field or the password changes", () => {
    stubServer({
      "GET /users/u1": { id: "u1", name: "jo", email: "jo@example.org" },
      "GET /users/u1/tokens": { tokens: [] },
    });

    const app = loggedInApp();
    const rendered = render(app);

    const save = rendered.querySelector<HTMLButtonElement>(".page-heading-actions button");
    expect(save?.textContent).toBe(app.t.t("action.save"));
    expect(save?.disabled).toBe(true);

    const displayName = rendered.querySelector<HTMLInputElement>("input[name='display_name']");
    if (!displayName || !save) {
      throw new Error("the profile form is missing its display name field");
    }

    displayName.value = "Jo the Second";
    displayName.dispatchEvent(new Event("input", { bubbles: true }));
    expect(save.disabled).toBe(false);
  });

  it("does not offer the administrator a way to delete the one admin account", () => {
    stubServer({
      "GET /users/u1": { id: "u1", name: "root" },
      "GET /users/u1/tokens": { tokens: [] },
    });

    const app = mountApp(() => []);
    app.session.set({ id: "u1", name: "root", display_name: "Root", is_admin: true });
    const rendered = render(app);
    const titles = [...rendered.querySelectorAll(".card-title")].map((entry) => entry.textContent);

    expect(titles).not.toContain(app.t.t("account.delete"));
    // And the name is not editable either: the installation refers to it.
    const name = labelled(rendered, app.t.t("auth.name")) as HTMLInputElement | null;
    expect(name?.disabled).toBe(true);
  });

  it("lists the tokens, and says so when there are none", async () => {
    stubServer({
      "GET /users/u1": { id: "u1", name: "jo" },
      "GET /users/u1/tokens": { tokens: [] },
    });

    const app = loggedInApp();
    const rendered = render(app);
    await settle();

    expect(rendered.textContent).toContain(app.t.t("account.tokens.none"));
  });

  it("refuses to delete the account until the name is typed exactly", async () => {
    stubServer({
      "GET /users/u1": { id: "u1", name: "jo" },
      "GET /users/u1/tokens": { tokens: [] },
      "GET /users/u1/deletion-impact": {
        active_orders: 1,
        active_items: 2,
        expired_items: 3,
        created_active_orders: 1,
      },
    });

    const app = loggedInApp();
    const rendered = render(app);
    await settle();

    const start = [...rendered.querySelectorAll("button")].find(
      (control) => control.textContent === app.t.t("account.delete"),
    );
    start?.click();
    await settle();

    const modal = document.querySelector(".modal");
    expect(modal).not.toBeNull();
    // The impact is named, not described in general terms.
    expect(modal?.textContent).toContain("2 items in open orders");
    expect(modal?.textContent).toContain("3 items in closed orders");

    const confirm = [...(modal?.querySelectorAll("button") ?? [])].find(
      (control) => control.textContent === app.t.t("account.delete"),
    );
    confirm?.click();
    await settle();

    // Nothing typed, so nothing happened.
    expect(modal?.textContent).toContain(app.t.t("account.delete.name_mismatch"));
    expect(app.session.user).toEqual(user);
  });
});
