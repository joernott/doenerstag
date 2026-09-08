// The administration screens and the content pages.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { contentPage, usersPage } from "../src/pages/admin";
import { fails, mountApp, settle, stubServer } from "./helpers";

const users = {
  users: [
    {
      id: "u1",
      name: "root",
      display_name: "Administrator",
      email: "",
      is_admin: true,
      created_at: "2026-01-01T09:00:00Z",
      last_login_at: "2026-09-06T08:00:00Z",
    },
    {
      id: "u2",
      name: "jo",
      display_name: "Jo Ott",
      email: "jo@example.org",
      is_admin: false,
      created_at: "2026-02-01T09:00:00Z",
    },
  ],
};

function asAdmin(): ReturnType<typeof mountApp> {
  const app = mountApp(() => []);
  app.session.set({ id: "u1", name: "root", display_name: "Administrator", is_admin: true });
  return app;
}

beforeEach(() => {
  document.body.replaceChildren();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the user administration", () => {
  it("tells a visitor who is not the administrator, rather than showing an empty table", async () => {
    stubServer({});
    const app = mountApp(() => []);
    const rendered = await usersPage(app);

    expect(rendered.textContent).toContain(app.t.t("error.3000"));
    expect(rendered.querySelector("a[href='/account']")).not.toBeNull();
  });

  it("lists the accounts with when they were made and last seen", async () => {
    stubServer({ "GET /users": users });

    const app = asAdmin();
    const rendered = await usersPage(app);
    await settle();

    const rows = [...rendered.querySelectorAll(".user-row")];
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("Administrator");
    expect(rows[1]?.textContent).toContain("Jo Ott");
    expect(rows[1]?.textContent).toContain(app.t.t("admin.never_logged_in"));
  });

  it("offers no way to delete the one administrator", async () => {
    stubServer({ "GET /users": users });

    const app = asAdmin();
    const rendered = await usersPage(app);
    await settle();

    const rows = [...rendered.querySelectorAll(".user-row")];
    // The server refuses both -- deleting the administrator and deleting
    // yourself -- so the button is not offered for either.
    expect(rows[0]?.querySelector("button")).toBeNull();
    expect(rows[1]?.querySelector("button")).not.toBeNull();
  });

  it("names the impact and wants the user name typed before deleting", async () => {
    const { calls } = stubServer({
      "GET /users": users,
      "GET /users/u2/deletion-impact": {
        active_orders: 1,
        active_items: 2,
        expired_items: 0,
        created_active_orders: 1,
      },
    });

    const app = asAdmin();
    const rendered = await usersPage(app);
    await settle();

    const remove = [...rendered.querySelectorAll("button")].find(
      (control) => control.textContent === app.t.t("action.delete"),
    );
    remove?.click();
    await settle();

    const modal = document.querySelector(".modal");
    expect(modal?.textContent).toContain("Jo Ott");
    expect(modal?.textContent).toContain("2 items in open orders");

    const confirm = [...(modal?.querySelectorAll("button") ?? [])].find(
      (control) => control.textContent === app.t.t("action.delete"),
    );
    confirm?.click();
    await settle();

    // Nothing typed, so nothing was deleted.
    expect(modal?.textContent).toContain(app.t.t("admin.name_mismatch"));
    expect(calls.some((call) => call.method === "DELETE")).toBe(false);

    const typed = modal?.querySelector("input");
    if (typed) {
      typed.value = "jo";
    }
    confirm?.click();
    await settle();

    expect(calls.some((call) => call.method === "DELETE" && call.path === "/users/u2")).toBe(true);
  });
});

describe("the content pages", () => {
  it("renders the stored snippet", async () => {
    stubServer({ "GET /pages/imprint": "<h2>Imprint</h2><p>Ott Consult</p>" });

    const app = mountApp(() => []);
    const rendered = await contentPage(app, "imprint");

    expect(rendered.querySelector(".content-page h2")?.textContent).toBe("Imprint");
    expect(rendered.textContent).toContain("Ott Consult");
  });

  it("offers the replacement only to the administrator", async () => {
    stubServer({ "GET /pages/imprint": "<p>nothing yet</p>" });

    const anonymous = mountApp(() => []);
    const asVisitor = await contentPage(anonymous, "imprint");
    expect(
      [...asVisitor.querySelectorAll("button")].map((control) => control.textContent),
    ).not.toContain(anonymous.t.t("admin.replace_content"));

    const admin = asAdmin();
    const asAdministrator = await contentPage(admin, "imprint");
    expect(
      [...asAdministrator.querySelectorAll("button")].map((control) => control.textContent),
    ).toContain(admin.t.t("admin.replace_content"));
  });

  it("shows what the server kept rather than what was typed", async () => {
    const { calls } = stubServer({
      "GET /pages/legal_notes": "<p>old</p>",
      // The server strips the script and answers with what it stored.
      "PUT /pages/legal_notes": "<p>new</p>",
    });

    const app = asAdmin();
    const rendered = await contentPage(app, "legal_notes");
    await settle();

    [...rendered.querySelectorAll("button")]
      .find((control) => control.textContent === app.t.t("admin.replace_content"))
      ?.click();
    await settle();

    const editor = document.querySelector<HTMLTextAreaElement>(".modal textarea");
    if (!editor) {
      throw new Error("no editor");
    }
    editor.value = "<p>new</p><script>alert(1)</script>";
    document.querySelector(".modal form")?.dispatchEvent(new Event("submit"));
    await settle();

    expect(calls.some((call) => call.method === "PUT")).toBe(true);
    // An administrator sees immediately that the script was not stored.
    expect(rendered.querySelector(".content-page")?.innerHTML).toBe("<p>new</p>");
    expect(document.querySelector(".modal")).toBeNull();
  });

  it("says so when the snippet is refused", async () => {
    stubServer({
      "GET /pages/imprint": "<p>old</p>",
      "PUT /pages/imprint": fails(400, {
        error: { code: 1001, message: "the snippet is empty once sanitised" },
      }),
    });

    const app = asAdmin();
    const rendered = await contentPage(app, "imprint");
    await settle();

    [...rendered.querySelectorAll("button")]
      .find((control) => control.textContent === app.t.t("admin.replace_content"))
      ?.click();
    await settle();

    document.querySelector(".modal form")?.dispatchEvent(new Event("submit"));
    await settle();

    const modal = document.querySelector(".modal");
    expect(modal).not.toBeNull();
    expect(modal?.textContent).toContain(app.t.t("error.1001"));
    // And the page still shows what was there before.
    expect(rendered.querySelector(".content-page")?.innerHTML).toBe("<p>old</p>");
  });
});
