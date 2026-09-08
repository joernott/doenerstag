// Entry point.
//
// Reads the two cookies that decide how the application looks, builds the
// application object, fetches what the server says about itself and starts
// routing. Everything else lives in the modules this pulls together.

import { api, type VersionInfo } from "./api";
import { createApp, type App } from "./app";
import { openModal } from "./components/modal";
import { LANGUAGE_COOKIE, readCookie } from "./cookies";
import { el } from "./dom";
import { notFoundPage, routes } from "./pages";
import { storedTheme } from "./theme";

async function main(): Promise<void> {
  const root = document.getElementById("app");
  if (!root) {
    return;
  }

  const app = createApp(
    root,
    { language: readCookie(LANGUAGE_COOKIE), theme: storedTheme() },
    navigator.languages ?? [navigator.language],
    routes,
    notFoundPage,
  );

  // The shell is rendered before either request finishes. docs/06_ui_ux.md
  // rules out loading states on a fast LAN, and a chrome that appears
  // immediately and fills in is better than an empty page that waits.
  app.render();
  app.router.start();

  // Who is logged in, and what the server is. Both are needed by the title bar
  // -- the account control by the first, the API documentation entry by the
  // second -- so the shell is rendered again once they arrive. They are asked
  // for together because neither depends on the other.
  const [, version] = await Promise.all([
    app.session.load(),
    api.get<VersionInfo>("/version").catch(() => null),
  ]);
  app.version = version;
  app.render();
  app.router.refresh();

  // After the render, so the dialog opens over the application rather than over
  // an empty shell.
  announceSessionEnded(app);
}

void main();
/**
 * Says so, when the server reports that the session ended.
 *
 * Somebody who left a tab open overnight arrives at a logged-out page they did
 * not ask for. Before this, the server refused every request that carried the
 * dead cookie, so they got a JSON error envelope instead of the application and
 * no way out of it short of clearing cookies by hand. Now they get the
 * application, anonymous, and one dialog explaining why.
 *
 * The message is the translation of the error code, so "your session expired"
 * and "you were logged in somewhere else" stay distinct -- they call for
 * different reactions, and the second is worth noticing.
 */
function announceSessionEnded(app: App): void {
  const ended = app.session.takeEndedNotice();
  if (!ended) {
    return;
  }

  const { t } = app;
  const reason = t.has(`error.${ended.code}`)
    ? t.t(`error.${ended.code}`)
    : t.t("session.ended.generic");

  const login = el("button", {
    type: "button",
    class: "button button-primary",
    text: t.t("auth.login"),
  });

  const dismiss = el("button", {
    type: "button",
    class: "button",
    text: t.t("session.ended.stay"),
  });

  const handle = openModal({
    title: t.t("session.ended.title"),
    body: el("p", { text: reason }),
    actions: [login, dismiss],
    closeLabel: t.t("action.close"),
  });

  login.addEventListener("click", () => {
    handle.close();
    app.router.navigate("/account");
  });
  dismiss.addEventListener("click", () => handle.close());
  login.focus();
}
