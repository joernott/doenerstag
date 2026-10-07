// The title bar and the main dropdown menu.
//
// The layout is the table in docs/06_ui_ux.md, left to right: logo, name,
// spacer, language selector, theme toggle, account control, menu button.

import type { App } from "../app";
import { currentPath, loginHref } from "../returnto";
import { el, icon } from "../dom";
import { logoMark } from "../logo";
import { languages } from "../i18n";

/** Builds the title bar. */
export function renderTitleBar(app: App): HTMLElement {
  const { t } = app;

  const brand = el(
    "a",
    { class: "brand", href: "/", "aria-label": t.t("nav.home") },
    logo(),
    el("span", { class: "brand-name", text: t.t("app.name") }),
  );

  const bar = el(
    "div",
    { class: "titlebar-inner" },
    brand,
    el("div", { class: "titlebar-spacer" }),
    languageSelector(app),
    themeToggle(app),
    accountControl(app),
    menuButton(app),
  );

  return el("header", { class: "titlebar" }, bar);
}

/**
 * The logo.
 *
 * The real mark now, from assets/doenerstag.svg, in place of the four strokes
 * that stood in for it. It is inlined rather than referenced, so the frame and
 * the calendar text follow the title bar's own colour and one file serves both
 * themes -- see src/logo.ts. The `.logo` class keeps the size it had.
 */
function logo(): Element {
  return logoMark({ class: "logo" });
}

/**
 * The language selector.
 *
 * Rendered from the registry, so a catalog added to the build appears here with
 * no change to this file, and hidden entirely when the build ships only one
 * language -- a dropdown with a single option is furniture, not a choice.
 * Each language is listed by its endonym: somebody looking for German is
 * looking for "Deutsch".
 *
 * A button and a list drawn by the page, like the main menu, and no longer a
 * native select (19.6). A native list is drawn by the browser, and Firefox lets
 * a page style neither the list nor the entry under the pointer: in the dark
 * theme that entry was black on black. Chrome and Edge could have been reached
 * with base-select, Firefox could not, and a selector that works in two of the
 * three browsers people here use does not work. The button shows the language
 * in force, so the list does not mark it a second time.
 */
function languageSelector(app: App): HTMLElement | null {
  const available = languages();
  if (available.length < 2) {
    return null;
  }

  const { t } = app;
  const listId = "language-menu";
  const current = available.find((meta) => meta.code === app.language);

  const button = el(
    "button",
    {
      type: "button",
      class: "language-button",
      "aria-label": `${t.t("language.select")}: ${current?.endonym ?? app.language}`,
      "aria-haspopup": "true",
      "aria-expanded": "false",
      "aria-controls": listId,
    },
    el("span", { text: current?.endonym ?? app.language, lang: app.language }),
    icon("chevron-down"),
  );

  const list = el("div", { class: "menu language-menu", id: listId, hidden: true });
  for (const meta of available) {
    list.appendChild(
      el("button", {
        type: "button",
        class: "menu-entry",
        text: meta.endonym,
        lang: meta.code,
        ...(meta.code === app.language ? { "aria-current": "true" } : {}),
        onclick: () => {
          app.setLanguage(meta.code);
        },
      }),
    );
  }

  return dropdown(button, list, "language-container");
}

/** The dark/light toggle. */
function themeToggle(app: App): HTMLElement {
  const dark = app.theme === "dark";
  return el(
    "button",
    {
      type: "button",
      class: "icon-button",
      "aria-label": app.t.t("theme.toggle"),
      title: app.t.t(dark ? "theme.light" : "theme.dark"),
      onclick: () => app.switchTheme(),
    },
    // The icon shows what a click switches to, not what is in force: a sun to
    // turn the lights on.
    icon(dark ? "sun" : "moon"),
  );
}

/** The account control: a login button, or the display name and a logout. */
function accountControl(app: App): HTMLElement {
  const { t } = app;
  const user = app.session.user;

  if (!user) {
    // Carries where it was pressed, so logging in returns there rather than
    // dumping somebody on their account page.
    return el("a", {
      class: "button button-quiet",
      href: loginHref(currentPath()),
      text: t.t("auth.login_or_register"),
    });
  }

  return el(
    "div",
    { class: "account" },
    el("a", { class: "account-name", href: "/account" }, icon("user"), el("span", { text: user.display_name })),
    el(
      "button",
      {
        type: "button",
        class: "icon-button",
        "aria-label": t.t("auth.logout"),
        title: t.t("auth.logout"),
        onclick: () => {
          void app.session.logout().then(() => {
            app.render();
            app.navigate("/");
            app.router.refresh();
          });
        },
      },
      icon("logout"),
    ),
  );
}

/** The main dropdown menu and the button that opens it. */
function menuButton(app: App): HTMLElement {
  const { t } = app;
  const menuId = "main-menu";

  const button = el(
    "button",
    {
      type: "button",
      class: "icon-button",
      "aria-label": t.t("nav.open_menu"),
      "aria-haspopup": "true",
      "aria-expanded": "false",
      "aria-controls": menuId,
    },
    icon("menu"),
  );

  const menu = el("nav", { class: "menu", id: menuId, hidden: true, "aria-label": t.t("nav.menu") });
  for (const entry of menuEntries(app)) {
    menu.appendChild(
      el("a", {
        class: "menu-entry",
        href: entry.href,
        text: entry.label,
        ...(entry.external ? { target: "_blank", rel: "noreferrer" } : {}),
      }),
    );
  }

  return dropdown(button, menu, "menu-container", (open) => {
    button.setAttribute("aria-label", t.t(open ? "nav.close_menu" : "nav.open_menu"));
  });
}

/**
 * A button that opens a panel below it: the main menu and the language list.
 *
 * Escape closes and returns focus to the button; a click anywhere else closes
 * without stealing it. Both live on the document, because the panel has to
 * close for events that never reach it -- and both are attached only while it
 * is open, because the title bar is rebuilt on every language, theme and
 * session change and listeners left behind would pile up on each of them.
 * Choosing an entry closes it too: a panel that stayed open across a
 * navigation would cover the page it just navigated to.
 */
function dropdown(
  button: HTMLButtonElement,
  panel: HTMLElement,
  containerClass: string,
  onToggle?: (open: boolean) => void,
): HTMLElement {
  const container = el("div", { class: containerClass }, button, panel);

  const onKeyDown = (event: KeyboardEvent): void => {
    if (event.key === "Escape") {
      setOpen(false);
      button.focus();
    }
  };
  const onDocumentClick = (event: MouseEvent): void => {
    const target = event.target;
    if (target instanceof Node && !container.contains(target)) {
      setOpen(false);
    }
  };

  const setOpen = (open: boolean): void => {
    if (open === !panel.hidden) {
      return;
    }
    panel.hidden = !open;
    button.setAttribute("aria-expanded", String(open));
    onToggle?.(open);

    if (open) {
      document.addEventListener("keydown", onKeyDown);
      document.addEventListener("click", onDocumentClick);
      panel.querySelector<HTMLElement>("a, button")?.focus();
      return;
    }
    document.removeEventListener("keydown", onKeyDown);
    document.removeEventListener("click", onDocumentClick);
  };

  button.addEventListener("click", () => setOpen(panel.hidden));
  panel.addEventListener("click", () => setOpen(false));

  return container;
}

interface MenuEntry {
  href: string;
  label: string;
  /**
   * Opened in a new tab.
   *
   * For the API documentation, which is Swagger UI rather than a page of this
   * application: following it in place loses whatever the reader was doing,
   * and coming back from it means the browser back button rather than the
   * navigation they were using a moment ago.
   */
  external?: boolean;
}

/**
 * The menu entries, filtered by who is looking.
 *
 * The table in docs/06_ui_ux.md decides visibility: my account for logged-in
 * users, the user administration for the administrator, and the API
 * documentation only when the server actually serves it -- which is why
 * /version reports whether Swagger is enabled.
 */
function menuEntries(app: App): MenuEntry[] {
  const { t } = app;
  const entries: MenuEntry[] = [
    { href: "/", label: t.t("nav.orders") },
    { href: "/restaurants", label: t.t("nav.restaurants") },
  ];

  if (app.session.isAuthenticated) {
    entries.push({ href: "/account", label: t.t("nav.account") });
  }
  if (app.session.isAdmin) {
    entries.push({ href: "/admin/users", label: t.t("nav.users") });
  }

  entries.push(
    { href: "/version", label: t.t("nav.version") },
    { href: "/imprint", label: t.t("nav.imprint") },
    { href: "/legal-notes", label: t.t("nav.legal_notes") },
  );

  if (app.version?.swagger !== false) {
    entries.push({ href: "/tools/swagger/", label: t.t("nav.api_docs"), external: true });
  }
  return entries;
}
