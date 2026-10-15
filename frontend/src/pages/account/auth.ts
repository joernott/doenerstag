// The account page for somebody logged out: a combined login and register
// panel with two tabs, the forgotten-password dialog, and the page a reset
// link opens. docs/06_ui_ux.md. Part of the account page.

import type { App } from "../../app";
import { api, errorMessage, type SessionUser } from "../../api";
import { NAME_COOKIE, readCookie, writeCookie } from "../../cookies";
import { el } from "../../dom";
import { button, field, fieldError, form, input } from "../../components/forms";
import { openFormDialog } from "../../components/formdialog";
import { openModal } from "../../components/modal";
import { passwordField } from "../../components/password";
import { tabs } from "../../components/tabs";
import { actions, page, section } from "../page";
import { statusLine } from "../../components/status";

/**
 * The login and register panel.
 *
 * Two tabs rather than two pages: somebody who came here to log in and finds
 * they have no account should not have to go looking for the other form.
 */
export function loginPanel(app: App, returnTo: string | null): HTMLElement {
  const { t } = app;

  // The shared component, like every other tab strip in the application. Two
  // implementations of one pattern is one too many, and a hand-written one
  // easily ends up with arrow keys that move but do not rove the tabindex.
  return page(
    t.t("auth.login_or_register"),
    tabs(
      [
        {
          id: "login",
          label: t.t("auth.login"),
          panel: loginForm(app, returnTo),
        },
        {
          id: "register",
          label: t.t("auth.register"),
          panel: registerForm(app),
        },
      ],
      { label: t.t("auth.login_or_register"), initial: 0 },
    ),
  );
}

function loginForm(app: App, returnTo: string | null): HTMLElement {
  const { t } = app;
  const status = statusLine();

  // Prefilled from the cookie docs/06_ui_ux.md describes: the last name used,
  // so a returning person types a password and nothing else.
  const name = input({
    name: "name",
    autocomplete: "username",
    required: true,
    value: readCookie(NAME_COOKIE) ?? "",
  });
  const password = passwordField({ t, autocomplete: "current-password" });
  const submit = button({
    label: t.t("auth.login"),
    variant: "primary",
    type: "submit",
  });

  // Behind the login button, because it is the thing somebody reaches for only
  // after the button in front of it has not worked.
  const forgotten = button({
    label: t.t("auth.forgot_password"),
    type: "button",
    onclick: () => forgottenPasswordDialog(app, name.value.trim()),
  });

  const element = form(
    () => {
      void send();
    },
    field({ label: t.t("auth.name"), control: name }),
    password.element,
    el("p", { class: "field-hint", text: t.t("auth.session_replaced") }),
    actions(submit, forgotten),
    status.element,
  );

  async function send(): Promise<void> {
    status.clear();
    submit.disabled = true;
    try {
      const body = await api.post<{ user: SessionUser }>("/auth/login", {
        name: name.value.trim(),
        password: password.value(),
      });
      writeCookie(NAME_COOKIE, name.value.trim());
      finishLogin(app, body.user, returnTo);
    } catch (error) {
      status.fail(errorMessage(t, error));
      password.control.focus();
    } finally {
      submit.disabled = false;
    }
  }

  return el("div", { class: "panel" }, element);
}

function registerForm(app: App): HTMLElement {
  const { t } = app;
  const status = statusLine();

  const name = input({
    name: "name",
    autocomplete: "username",
    required: true,
  });
  const displayName = input({ name: "display_name", autocomplete: "nickname" });
  const email = input({ name: "email", type: "email", autocomplete: "email" });
  const password = passwordField({ t, indicator: true });
  const submit = button({
    label: t.t("auth.register"),
    variant: "primary",
    type: "submit",
  });

  const element = form(
    () => {
      void send();
    },
    field({
      label: t.t("auth.name"),
      control: name,
      hint: t.t("auth.name_hint"),
    }),
    field({
      label: t.t("auth.display_name"),
      control: displayName,
      hint: t.t("auth.display_name_hint"),
      optionalLabel: t.t("auth.optional"),
    }),
    field({
      label: t.t("auth.email"),
      control: email,
      hint: t.t("auth.email_hint"),
      optionalLabel: t.t("auth.optional"),
    }),
    password.element,
    actions(submit),
    status.element,
  );

  async function send(): Promise<void> {
    status.clear();
    if (!password.acceptable()) {
      // The rules are already on the screen, filling in as they are met. An
      // error message here would only repeat them.
      password.control.focus();
      return;
    }

    submit.disabled = true;
    try {
      const body = await api.post<{ user: SessionUser }>("/auth/register", {
        name: name.value.trim(),
        display_name: displayName.value.trim(),
        email: email.value.trim(),
        password: password.value(),
      });
      writeCookie(NAME_COOKIE, name.value.trim());
      finishLogin(app, body.user);
    } catch (error) {
      status.fail(errorMessage(t, error));
    } finally {
      submit.disabled = false;
    }
  }

  return el("div", { class: "panel" }, element);
}

/** Records the new session and rebuilds everything that depends on it. */
function finishLogin(app: App, user: SessionUser, returnTo: string | null = null): void {
  app.session.set(user);
  app.render();

  // Back where the login link was pressed, if it carried somewhere. Registering
  // passes nothing and so stays here, which is where a new account has a
  // display name and an address to fill in.
  if (returnTo !== null) {
    app.router.navigate(returnTo);
    return;
  }
  app.router.refresh();
}

/**
 * Asks for a reset link.
 *
 * The answer is the same whether the account exists or not, which is the
 * server's rule and not a detail this dialog may soften: a message that says
 * "we have sent you a mail" for a name nobody has is the point, because the
 * alternative turns the login page into a way to find out who works here.
 *
 * So the confirmation is worded to be true either way. It does not say a
 * message was sent; it says one was sent if the account exists and has an
 * address.
 */
function forgottenPasswordDialog(app: App, prefill: string): void {
  const { t } = app;
  const who = input({
    name: "name",
    autocomplete: "username",
    required: true,
    value: prefill,
  });
  const missing = fieldError(who);

  openFormDialog({
    t,
    title: t.t("auth.forgot_password"),
    submitLabel: t.t("auth.send_reset_link"),
    focus: who,
    fields: [
      el("p", { text: t.t("auth.forgot_password_explain") }),
      field({ label: t.t("auth.name_or_email"), control: who, error: missing }),
    ],
    onSubmit: async () => {
      const value = who.value.trim();
      if (!value) {
        missing.set(t.t("auth.name_or_email_required"));
        who.focus();
        return false;
      }
      missing.clear();
      await api.post("/auth/password-reset", { name: value });
      return true;
    },
    after: () => {
      openModal({
        title: t.t("auth.reset_link_sent"),
        body: el("p", { text: t.t("auth.reset_link_sent_explain") }),
        actions: [],
        closeLabel: t.t("action.close"),
      });
    },
  });
}

/**
 * The page a reset link opens.
 *
 * The token is never shown and never put in a field: it is in the address bar
 * already, and repeating it would only invite somebody to copy the wrong half
 * of it. Whether it is any good is not asked in advance either -- a "check this
 * token" endpoint would be a way to test tokens -- so the first thing that
 * judges it is the submission itself, which is also the only moment it matters.
 */
export function resetPasswordPage(app: App, token: string): HTMLElement {
  const { t } = app;
  const status = statusLine();

  const password = passwordField({ t, autocomplete: "new-password" });
  const submit = button({
    label: t.t("auth.set_password"),
    variant: "primary",
    type: "submit",
  });

  const element = form(
    () => {
      void send();
    },
    el("p", { text: t.t("auth.reset_explain") }),
    password.element,
    actions(submit),
    status.element,
  );

  async function send(): Promise<void> {
    status.clear();
    submit.disabled = true;
    try {
      await api.post(`/auth/password-reset/${encodeURIComponent(token)}`, {
        password: password.value(),
      });
      // Straight to the login page, which is where somebody who has just
      // chosen a password wants to be. The session is not started for them:
      // the reset ended every session of the account, and starting a new one
      // from a link that arrived by mail would undo that.
      app.router.navigate("/account");
      openModal({
        title: t.t("auth.password_changed"),
        body: el("p", { text: t.t("auth.password_changed_explain") }),
        actions: [],
        closeLabel: t.t("action.close"),
      });
    } catch (error) {
      status.fail(errorMessage(t, error));
      password.control.focus();
    } finally {
      submit.disabled = false;
    }
  }

  return page(t.t("auth.set_password"), section(t.t("auth.set_password"), element));
}
