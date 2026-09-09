// The administration screens: the user list, and the two content pages.
//
// Both are the administrator's alone. A visitor who is not one is told so
// rather than shown an empty table: the menu does not offer these pages, but a
// URL can be typed.

import type { App } from "../app";
import { api, errorMessage, getList } from "../api";
import { currentPath, loginHref } from "../returnto";
import { append, el } from "../dom";
import { formatDateTime } from "../format";
import { button, field, form, input, textarea } from "../components/forms";
import { confirmDialog, openModal } from "../components/modal";
import { actions, page, section, statusLine } from "./page";

interface AdminUser {
  id: string;
  name: string;
  display_name: string;
  email: string;
  is_admin: boolean;
  created_at: string;
  last_login_at?: string;
}

interface DeletionImpact {
  active_orders: number;
  active_items: number;
  expired_items: number;
  created_active_orders: number;
}

/** The page a non-administrator gets for an administration URL. */
function forbidden(app: App): HTMLElement {
  const { t } = app;
  return page(
    t.t("nav.users"),
    section(
      t.t("error.title"),
      el("p", { text: t.t("error.3000") }),
      app.session.isAuthenticated
        ? null
        : el("p", {}, el("a", { class: "link", href: loginHref(currentPath()), text: t.t("auth.login") })),
    ),
  );
}

/** The user administration. */
export async function usersPage(app: App): Promise<HTMLElement> {
  const { t } = app;
  if (!app.session.isAdmin) {
    return forbidden(app);
  }

  const status = statusLine();
  const list = el("div", { class: "rows" });

  async function reload(): Promise<void> {
    try {
      const users = await getList<AdminUser>("/users", "users");
      render(users);
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  function render(users: AdminUser[]): void {
    const rows = users.map((user) => userRow(user));
    list.replaceChildren(...rows);
  }

  function userRow(user: AdminUser): HTMLElement {
    const own = user.id === app.session.user?.id;

    return el(
      "div",
      { class: "row user-row" },
      el(
        "div",
        { class: "user-identity" },
        el("strong", { text: user.display_name || user.name }),
        el("span", { class: "muted", text: user.name }),
        user.is_admin ? el("span", { class: "chip", text: t.t("nav.users") }) : null,
      ),
      el(
        "dl",
        { class: "definitions user-meta" },
        el("dt", { text: t.t("account.token.created") }),
        el("dd", { text: formatDateTime(app.language, user.created_at) }),
        el("dt", { text: t.t("admin.last_login") }),
        el("dd", {
          text: user.last_login_at
            ? formatDateTime(app.language, user.last_login_at)
            : t.t("admin.never_logged_in"),
        }),
      ),
      el(
        "div",
        { class: "actions user-actions" },
        button({
          label: t.t("action.edit"),
          onclick: () => {
            edit(user);
          },
        }),
        // The same thing the login page's link does, and deliberately the same
        // thing: an administrator pressing this is standing next to somebody
        // who cannot log in, and the account holder still chooses their own
        // password rather than being told one.
        button({
          label: t.t("admin.reset_password"),
          onclick: () => {
            void resetPassword(user);
          },
        }),
        // The one administrator cannot be deleted, and neither can the account
        // doing the deleting: both refusals belong to the server, and offering
        // a button for them would only invite the error.
        user.is_admin || own
          ? null
          : button({
              label: t.t("action.delete"),
              variant: "danger",
              onclick: () => {
                void remove(user);
              },
            }),
      ),
    );
  }

  /**
   * Editing somebody else's account.
   *
   * Name, display name and e-mail. Not the password: an administrator who sets
   * one knows it, and a password two people know is not a password. The button
   * next to this is how a password gets changed.
   */
  function edit(user: AdminUser): void {
    const name = input({ name: "name", required: true, value: user.name });
    const displayName = input({ name: "display_name", value: user.display_name });
    const email = input({ name: "email", type: "email", value: user.email ?? "" });
    const problem = el("p", { class: "field-error", role: "alert" });

    const save = button({
      label: t.t("action.save"),
      variant: "primary",
      onclick: () => {
        void perform();
      },
    });
    const cancel = button({ label: t.t("action.cancel"), onclick: () => modal.close() });

    const modal = openModal({
      title: t.t("admin.edit_user"),
      closeLabel: t.t("action.close"),
      className: "modal-narrow",
      body: [
        field({ label: t.t("auth.name"), control: name }),
        field({ label: t.t("auth.display_name"), control: displayName }),
        field({ label: t.t("auth.email"), control: email }),
        problem,
      ],
      actions: [save, cancel],
    });
    name.focus();

    async function perform(): Promise<void> {
      problem.textContent = "";
      save.disabled = true;
      try {
        await api.patch(`/users/${user.id}`, {
          name: name.value.trim(),
          display_name: displayName.value.trim(),
          email: email.value.trim(),
        });
        modal.close();
        await reload();
      } catch (error) {
        problem.textContent = errorMessage(t, error);
      } finally {
        save.disabled = false;
      }
    }
  }

  /**
   * Sending somebody a reset link.
   *
   * This calls the same endpoint the login page's "Forgot your password?" does,
   * which means it inherits that endpoint's rule: the answer says nothing about
   * whether the account exists or has an address. Here the administrator can
   * see both on the row in front of them, so the confirmation says what will
   * actually have happened rather than the careful wording the login page needs.
   */
  async function resetPassword(user: AdminUser): Promise<void> {
    status.clear();

    const confirmed = await confirmDialog({
      t,
      title: t.t("admin.reset_password"),
      message: user.email
        ? t.t("admin.reset_password_confirm", { name: user.name, email: user.email })
        : t.t("admin.reset_password_no_email", { name: user.name }),
      confirmLabel: t.t("admin.reset_password"),
      danger: false,
    });
    if (!confirmed) {
      return;
    }

    try {
      await api.post("/auth/password-reset", { name: user.name });
    } catch (error) {
      status.fail(errorMessage(t, error));
      return;
    }

    status.say(
      user.email
        ? t.t("admin.reset_password_sent", { email: user.email })
        : t.t("admin.reset_password_not_sent"),
    );
  }

  /**
   * Deleting somebody else's account.
   *
   * The same impact-first flow the owner gets: the server is asked what it
   * would cost, the modal names it, and the administrator types the user name
   * to confirm. An administrator deleting the wrong account by a misclick is
   * exactly the accident this prevents.
   */
  async function remove(user: AdminUser): Promise<void> {
    status.clear();

    let impact: DeletionImpact;
    try {
      impact = await api.get<DeletionImpact>(`/users/${user.id}/deletion-impact`);
    } catch (error) {
      status.fail(errorMessage(t, error));
      return;
    }

    const typed = input({ autocomplete: "off" });
    const problem = el("p", { class: "field-error", role: "alert" });

    const confirm = button({
      label: t.t("action.delete"),
      variant: "danger",
      onclick: () => {
        if (typed.value.trim() !== user.name) {
          problem.textContent = t.t("admin.name_mismatch");
          typed.focus();
          return;
        }
        void perform();
      },
    });
    const cancel = button({ label: t.t("action.cancel"), onclick: () => modal.close() });

    const lines = [
      impact.active_items > 0
        ? t.t("account.delete.impact.active_items", { count: impact.active_items })
        : null,
      impact.expired_items > 0
        ? t.t("account.delete.impact.expired_items", { count: impact.expired_items })
        : null,
      impact.created_active_orders > 0
        ? t.t("account.delete.impact.orders", { count: impact.created_active_orders })
        : null,
    ].filter((line): line is string => line !== null);

    const modal = openModal({
      title: t.t("admin.delete_user"),
      closeLabel: t.t("action.close"),
      className: "modal-narrow",
      body: [
        el("p", { text: `${user.display_name || user.name} (${user.name})` }),
        lines.length > 0
          ? el("ul", { class: "impact" }, ...lines.map((line) => el("li", { text: line })))
          : el("p", { class: "muted", text: t.t("account.delete.impact.none") }),
        field({ label: t.t("admin.confirm_name"), control: typed }),
        problem,
      ],
      actions: [cancel, confirm],
    });
    cancel.focus();

    async function perform(): Promise<void> {
      try {
        await api.delete(`/users/${user.id}`);
        modal.close();
        await reload();
      } catch (error) {
        problem.textContent = errorMessage(t, error);
      }
    }
  }

  await reload();
  return page(t.t("nav.users"), section(t.t("nav.users"), list, status.element));
}

/**
 * The imprint and the legal notes.
 *
 * The stored snippet is HTML the administrator supplied, sanitised by the
 * server on the way in with the same allow-list the installer uses. This is the
 * one place in the application that assigns innerHTML, and it says so.
 */
export async function contentPage(app: App, key: "imprint" | "legal_notes"): Promise<HTMLElement> {
  const { t } = app;
  const title = t.t(key === "imprint" ? "nav.imprint" : "nav.legal_notes");
  const status = statusLine();

  const body = el("div", { class: "content-page" });

  async function load(): Promise<void> {
    try {
      body.innerHTML = await api.text(`/pages/${key}`);
    } catch (error) {
      body.replaceChildren(el("p", { class: "field-error", text: errorMessage(t, error) }));
    }
  }

  await load();

  const controls = app.session.isAdmin
    ? actions(
        button({
          label: t.t("admin.replace_content"),
          onclick: () => {
            openEditor();
          },
        }),
      )
    : null;

  function openEditor(): void {
    const editorStatus = statusLine();
    const html = textarea({ rows: 16 });
    html.value = body.innerHTML.trim();

    const save = button({ label: t.t("action.save"), variant: "primary", type: "submit" });

    const editor = form(
      () => {
        void store();
      },
      field({
        label: t.t("admin.snippet"),
        control: html,
        hint: t.t("admin.snippet_hint"),
      }),
      editorStatus.element,
    );

    async function store(): Promise<void> {
      editorStatus.clear();
      save.disabled = true;
      try {
        // The server answers with what it kept, which is what the page then
        // shows: an administrator sees immediately that their <script> was not
        // stored, rather than discovering it later.
        body.innerHTML = await api.putText(`/pages/${key}`, html.value);
        modal.close();
        status.say(t.t("state.saved"));
      } catch (error) {
        editorStatus.fail(errorMessage(t, error));
      } finally {
        save.disabled = false;
      }
    }

    const modal = openModal({
      title: t.t("admin.replace_content"),
      closeLabel: t.t("action.close"),
      body: editor,
      actions: [button({ label: t.t("action.cancel"), onclick: () => modal.close() }), save],
    });
    editor.id = `content-form-${key}`;
    save.setAttribute("form", editor.id);
    html.focus();
  }

  // A card without a heading of its own: the page already has the title as its
  // h1, and repeating it as an h2 would announce it twice to a screen reader
  // and read as a mistake to everybody else.
  const card = el("section", { class: "card" });
  append(card, body, controls, status.element);
  return page(title, card);
}
