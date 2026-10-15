// The user page.
//
// Anonymous: a combined login and register panel with two tabs (auth.ts).
// Logged in: the profile, the password, the API tokens and the way out.
// docs/06_ui_ux.md.

import type { App } from "../../app";
import { api, errorMessage, type SessionUser } from "../../api";
import { el, type Child } from "../../dom";
import { formatDateTime } from "../../format";
import { button, field, form, input } from "../../components/forms";
import { openDeleteAccountDialog } from "../../components/deleteaccount";
import { copyText } from "../../components/clipboard";
import { dirtyTracker, saveAllButton, type EditableSection } from "../../components/editable";
import { confirmDialog, openModal } from "../../components/modal";
import { passwordField } from "../../components/password";
import { tabs } from "../../components/tabs";
import { actions, card, pageWithActions } from "../page";
import type { DeletionImpact } from "../../types";
import { runAction, statusLine } from "../../components/status";
import { loginPanel } from "./auth";

export { resetPasswordPage } from "./auth";

/** The account page, in whichever of its two shapes applies. */
export function accountPage(app: App, returnTo: string | null = null): HTMLElement {
  return app.session.user ? loggedIn(app, app.session.user) : loginPanel(app, returnTo);
}

/**
 * The account page for somebody logged in.
 *
 * The same shape as the restaurant page: tabs, with Save and Delete on the
 * title's line. Save covers the whole account rather than one tab of it, which
 * is why the profile and the password forms no longer carry one each -- two
 * buttons called Save on one page, each covering a different part of it, is a
 * question nobody should have to answer.
 */
function loggedIn(app: App, user: SessionUser): HTMLElement {
  const { t } = app;

  const profile = profileSection(app, user);
  const password = passwordSection(app, user);

  return pageWithActions(
    t.t("nav.account"),
    accountControls(app, user, profile, password),
    tabs(
      [
        {
          id: "profile",
          label: t.t("account.profile"),
          panel: profile.element,
        },
        {
          id: "password",
          label: t.t("account.password"),
          panel: password.element,
        },
        {
          id: "tokens",
          label: t.t("account.tokens"),
          panel: tokensSection(app, user),
        },
      ],
      { label: t.t("account.sections"), initial: 0 },
    ),
  );
}

/** Save and Delete, on the account title's line. */
function accountControls(
  app: App,
  user: SessionUser,
  profile: EditableSection,
  password: EditableSection,
): HTMLElement {
  const { t } = app;

  const save = saveAllButton(t, [profile, password]);

  const controls: Child[] = [save];

  // The administrator account cannot be deleted, and the server says so with
  // error 3000. Not offering the button at all is the "nothing that cannot be
  // done is shown as available" rule from docs/06_ui_ux.md; unlike the
  // restaurant's delete, there is no state in which this one becomes available,
  // so there is nothing a disabled button could usefully explain.
  if (!user.is_admin) {
    controls.push(deleteControl(app, user));
  }

  return el("div", { class: "page-heading-actions" }, ...controls);
}

function profileSection(app: App, user: SessionUser): EditableSection {
  const { t } = app;
  const status = statusLine();

  const name = input({ name: "name", value: user.name, required: true });
  const displayName = input({ name: "display_name", value: user.display_name });
  const email = input({ name: "email", type: "email" });

  // The e-mail address is not in the session -- the public profile does not
  // carry it -- so it is fetched for the form that edits it.
  void api
    .get<{ email?: string }>(`/users/${user.id}`)
    .then((body) => {
      email.value = body.email ?? "";
      // The address arriving is not somebody editing: it is the form finishing
      // loading, and Save must stay quiet until there is a real change.
      tracker.saved();
    })
    .catch(() => {
      // Leaving it empty would offer to clear an address that is set, so the
      // field is disabled instead until the page is reloaded.
      email.disabled = true;
    });

  // root cannot be renamed: the installation refers to it by name, and the
  // server refuses. Disabled with the reason rather than silently rejected.
  if (user.is_admin) {
    name.disabled = true;
    name.title = t.t("error.3003");
  }

  // Compared against a snapshot, so typing a character and deleting it again
  // leaves Save disabled.
  const snapshot = (): string =>
    JSON.stringify([name.value.trim(), displayName.value.trim(), email.value.trim()]);

  const tracker = dirtyTracker(snapshot);
  const touched = tracker.touched;
  for (const control of [name, displayName, email]) {
    control.addEventListener("input", touched);
  }

  const element = card(
    // Still a form, so Enter in a field saves. The button that submits it is on
    // the page heading.
    form(
      () => {
        void save();
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
        optionalLabel: t.t("auth.optional"),
      }),
      status.element,
    ),
  );

  async function save(): Promise<void> {
    status.clear();
    try {
      const updated = await api.patch<SessionUser>(`/users/${user.id}`, {
        ...(name.disabled ? {} : { name: name.value.trim() }),
        display_name: displayName.value.trim(),
        ...(email.disabled ? {} : { email: email.value.trim() }),
      });
      app.session.set(updated);
      // The title bar shows the display name, so it has to be rebuilt.
      app.render();
      status.say(t.t("state.saved"));
      tracker.saved();
    } catch (error) {
      status.fail(errorMessage(t, error));
      throw error;
    }
  }

  return {
    element,
    save,
    onDirtyChange: tracker.onDirtyChange,
  };
}

function passwordSection(app: App, user: SessionUser): EditableSection {
  const { t } = app;
  const status = statusLine();

  const current = passwordField({
    t,
    label: t.t("auth.current_password"),
    name: "current_password",
    autocomplete: "current-password",
  });
  const next = passwordField({
    t,
    label: t.t("auth.new_password"),
    name: "password",
    indicator: true,
  });
  // "Dirty" here means a new password has been typed. There is nothing to save
  // otherwise, and the current-password field on its own changes nothing.
  const tracker = dirtyTracker(() => next.control.value);
  const touched = tracker.touched;
  for (const control of [current.control, next.control]) {
    control.addEventListener("input", touched);
  }

  const element = card(
    form(
      () => {
        void save();
      },
      current.element,
      next.element,
      status.element,
    ),
  );

  async function save(): Promise<void> {
    status.clear();
    if (!next.acceptable()) {
      next.control.focus();
      // Refused before it was sent, which is still a refusal: throwing leaves
      // the page's Save enabled so it can be tried again with a better one.
      throw new Error("the new password does not meet the rules");
    }

    try {
      await api.patch(`/users/${user.id}`, {
        password: next.value(),
        current_password: current.value(),
      });
      current.control.value = "";
      next.control.value = "";
      next.control.dispatchEvent(new Event("input"));
      status.say(t.t("state.saved"));
    } catch (error) {
      status.fail(errorMessage(t, error));
      throw error;
    }
  }

  return {
    element,
    save,
    onDirtyChange: tracker.onDirtyChange,
  };
}

interface TokenBody {
  id: string;
  name: string;
  prefix: string;
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
}

function tokensSection(app: App, user: SessionUser): HTMLElement {
  const { t } = app;
  const status = statusLine();
  const list = el("div", { class: "token-list" });

  const name = input({ name: "token_name", required: true });
  const expires = input({ name: "expires_at", type: "date" });
  const submit = button({ label: t.t("account.token.new"), type: "submit" });

  function render(tokens: TokenBody[]): void {
    if (tokens.length === 0) {
      list.replaceChildren(el("p", { class: "muted", text: t.t("account.tokens.none") }));
      return;
    }

    const rows = tokens.map((token) =>
      el(
        "li",
        { class: "token" },
        el(
          "div",
          { class: "token-name" },
          el("strong", { text: token.name }),
          el("code", { class: "token-prefix", text: token.prefix }),
        ),
        el(
          "dl",
          { class: "definitions token-meta" },
          el("dt", { text: t.t("account.token.created") }),
          el("dd", { text: formatDateTime(app.language, token.created_at) }),
          el("dt", { text: t.t("account.token.expires") }),
          el("dd", {
            text: token.expires_at
              ? formatDateTime(app.language, token.expires_at)
              : t.t("account.token.never"),
          }),
          el("dt", { text: t.t("account.token.last_used") }),
          el("dd", {
            text: token.last_used_at
              ? formatDateTime(app.language, token.last_used_at)
              : t.t("account.token.unused"),
          }),
        ),
        button({
          label: t.t("account.token.revoke"),
          variant: "danger",
          onclick: () => revoke(token),
        }),
      ),
    );
    list.replaceChildren(el("ul", { class: "plain-list" }, ...rows));
  }

  async function reload(): Promise<void> {
    try {
      const body = await api.get<{ tokens: TokenBody[] }>(`/users/${user.id}/tokens`);
      render(body.tokens);
    } catch (error) {
      status.fail(errorMessage(t, error));
    }
  }

  async function revoke(token: TokenBody): Promise<void> {
    const agreed = await confirmDialog({
      t,
      message: token.name,
      confirmLabel: t.t("account.token.revoke"),
    });
    if (!agreed) {
      return;
    }
    await runAction({ status, t }, async () => {
      await api.delete(`/users/${user.id}/tokens/${token.id}`);
      await reload();
    });
  }

  async function create(): Promise<void> {
    await runAction({ status, t, busy: submit }, async () => {
      const created = await api.post<TokenBody & { token: string }>(`/users/${user.id}/tokens`, {
        name: name.value.trim(),
        // A date input gives a plain date; the API wants RFC 3339. End of
        // that day rather than its start, or "expires today" would mean
        // "expired this morning".
        expires_at: expires.value ? `${expires.value}T23:59:59Z` : "",
      });
      name.value = "";
      expires.value = "";
      showToken(app, created.token);
      await reload();
    });
  }

  void reload();

  const creator = form(
    () => {
      void create();
    },
    field({ label: t.t("account.token.name"), control: name }),
    field({
      label: t.t("account.token.expires"),
      control: expires,
      optionalLabel: t.t("auth.optional"),
    }),
    actions(submit),
  );

  return card(
    el("p", { class: "muted", text: t.t("account.tokens.intro") }),
    list,
    creator,
    status.element,
  );
}

/**
 * Shows a freshly created token.
 *
 * A modal rather than a line on the page, because this is the only time the
 * value exists: the server keeps a hash. Copying is offered, and the value is
 * selectable for a browser where the clipboard is not available.
 */
function showToken(app: App, value: string): void {
  const { t } = app;
  const valueInput = el("input", { class: "input", readonly: true, value });

  const copy = button({
    label: t.t("action.copy"),
    onclick: () => {
      valueInput.select();
      void copyText(value).then((copied) => {
        // When it could not, the value is selected, so ctrl-c works.
        if (copied) {
          copy.textContent = t.t("action.copied");
        }
      });
    },
  });

  const modal = openModal({
    title: t.t("account.token.new"),
    closeLabel: t.t("action.close"),
    className: "modal-narrow",
    body: [el("p", { text: t.t("account.token.once") }), valueInput, actions(copy)],
    actions: [button({ label: t.t("action.close"), onclick: () => modal.close() })],
  });
  valueInput.select();
}

/**
 * The Delete button for the account, for the page heading.
 *
 * The impact is fetched before the modal opens, so the confirmation names what
 * will actually happen rather than describing it in general terms, and the
 * person types their own user name to confirm. Both are docs/06_ui_ux.md.
 *
 * The warning that this cannot be undone used to be a card of its own with a
 * paragraph and a button in it. The modal already says the same thing, and says
 * it at the moment it matters -- after the button is pressed and before
 * anything happens -- so the card was a permanent warning about something
 * nobody had asked to do yet.
 */
function deleteControl(app: App, user: SessionUser): HTMLElement {
  const { t } = app;

  const start = button({
    label: t.t("account.delete"),
    variant: "danger",
    onclick: () => begin(),
  });

  async function begin(): Promise<void> {
    let impact: DeletionImpact;
    try {
      impact = await api.get<DeletionImpact>(`/users/${user.id}/deletion-impact`);
    } catch (error) {
      // Nowhere on the heading to report this, so it goes where the rest of the
      // deletion conversation happens: the dialog it was about to open.
      openModal({
        title: t.t("account.delete"),
        closeLabel: t.t("action.close"),
        className: "modal-narrow",
        body: el("p", {
          class: "field-error",
          role: "alert",
          text: errorMessage(t, error),
        }),
      });
      return;
    }
    openDeleteModal(impact);
  }

  function openDeleteModal(impact: DeletionImpact): void {
    openDeleteAccountDialog({
      t,
      title: t.t("account.delete"),
      intro: t.t("account.delete.intro"),
      impact,
      name: user.name,
      confirmLabel: t.t("account.delete.confirm_name"),
      mismatch: t.t("account.delete.name_mismatch"),
      remove: () => api.delete(`/users/${user.id}`),
      after: () => {
        // The server has already cleared the session cookies; this makes the
        // page agree with that rather than waiting for a 2002 to discover it.
        app.session.set(null);
        app.render();
        app.navigate("/");
        app.router.refresh();
      },
    });
  }

  return start;
}
