// Entry point.
//
// Reads the two cookies that decide how the application looks, builds the
// application object, fetches what the server says about itself and starts
// routing. Everything else lives in the modules this pulls together.

import { api, type VersionInfo } from "./api";
import { createApp } from "./app";
import { LANGUAGE_COOKIE, readCookie } from "./cookies";
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
}

void main();
