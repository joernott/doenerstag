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
  // A page test that moves the address moves it for the whole file otherwise,
  // and pages read the address when they build their login links.
  history.replaceState(null, "", "/");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the user administration", () => {
  it("tells a visitor who is not the administrator, rather than showing an empty table", async () => {
    stubServer({});
    // The link is built from the address the page is at, so the page has to be
    // at one.
    history.replaceState(null, "", "/admin/users");
    const app = mountApp(() => []);
    const rendered = await usersPage(app);

    expect(rendered.textContent).toContain(app.t.t("error.3000"));
    expect(
      rendered.querySelector("a[href='/account?next=%2Fadmin%2Fusers']"),
    ).not.toBeNull();
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
    // yourself -- so the button is not offered for either. Every row still has
    // the other two, Edit and Reset password, which is why this looks for the
    // delete button by its label rather than for any button at all.
    const deleteButton = (row: Element | undefined) =>
      [...(row?.querySelectorAll("button") ?? [])].find(
        (b) => b.textContent === app.t.t("action.delete"),
      ) ?? null;

    expect(deleteButton(rows[0])).toBeNull();
    expect(deleteButton(rows[1])).not.toBeNull();
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

/*
 * 18.5: the password dialog offers a reset link and, for somebody the link
 * cannot reach, a password set directly. The buttons are pressed, because a
 * footer button that is not linked to its form does nothing -- which is what
 * 18.1 found in two other dialogs.
 */
describe("the administrator's password dialog", () => {
  async function openFor(app: ReturnType<typeof mountApp>, name: string): Promise<HTMLElement> {
    const rendered = await usersPage(app);
    await settle();
    const row = [...rendered.querySelectorAll<HTMLElement>(".user-row")].find((entry) =>
      entry.textContent?.includes(name),
    );
    const open = [...(row?.querySelectorAll("button") ?? [])].find(
      (button) => button.textContent === app.t.t("admin.reset_password"),
    );
    open!.click();
    await settle();
    return document.querySelector<HTMLElement>("[role='dialog']")!;
  }

  function buttonIn(dialog: HTMLElement, label: string): HTMLButtonElement | undefined {
    return [...dialog.querySelectorAll("button")].find((button) => button.textContent === label);
  }

  it("sets a password typed into it, and closes", async () => {
    const stubs = stubServer({ "GET /users": users, "PATCH /users/u2": users.users[1] });
    const app = asAdmin();
    const dialog = await openFor(app, "Jo Ott");

    const field = dialog.querySelector<HTMLInputElement>("input[type='password']");
    field!.value = "Neues-Passwort-2026";
    buttonIn(dialog, app.t.t("admin.set_password"))!.click();
    await settle();

    const call = stubs.calls.find((entry) => entry.method === "PATCH");
    expect(call?.path).toBe("/users/u2");
    expect(call?.body).toEqual({ password: "Neues-Passwort-2026" });
    expect(document.querySelector("[role='dialog']")).toBeNull();
  });

  it("keeps the dialog open and says why when the server refuses the password", async () => {
    stubServer({
      "GET /users": users,
      "PATCH /users/u2": fails(400, { error: { code: 1006, message: "too weak", field: "password" } }),
    });
    const app = asAdmin();
    const dialog = await openFor(app, "Jo Ott");

    dialog.querySelector<HTMLInputElement>("input[type='password']")!.value = "kurz";
    buttonIn(dialog, app.t.t("admin.set_password"))!.click();
    await settle();

    expect(document.querySelector("[role='dialog']")).not.toBeNull();
    // The failure is in the dialog's own status line, where it can be read
    // beside the field, rather than behind the dialog on the page.
    const problem = dialog.querySelector(".status.status-error");
    expect(problem?.textContent).toBeTruthy();
  });

  it("still sends a reset link to somebody with an address", async () => {
    const stubs = stubServer({ "GET /users": users, "POST /auth/password-reset": {} });
    const app = asAdmin();
    const dialog = await openFor(app, "Jo Ott");

    const send = buttonIn(dialog, app.t.t("admin.send_reset_link"));
    expect(send?.disabled).toBe(false);
    send!.click();
    await settle();

    const call = stubs.calls.find((entry) => entry.method === "POST");
    expect(call?.path).toBe("/auth/password-reset");
    expect(call?.body).toEqual({ name: "jo" });
    expect(document.querySelector("[role='dialog']")).toBeNull();
  });

  it("does not offer a link to somebody with no address, and says why", async () => {
    stubServer({ "GET /users": users });
    const app = asAdmin();
    const dialog = await openFor(app, "Administrator");

    const send = buttonIn(dialog, app.t.t("admin.send_reset_link"));
    expect(send?.disabled).toBe(true);
    expect(send?.getAttribute("title")).toBe(app.t.t("admin.reset_password_no_email", { name: "root" }));
    // Setting one is the way in for this person, and it is offered.
    expect(dialog.querySelector("input[type='password']")).not.toBeNull();
  });
});
