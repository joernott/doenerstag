// The application object.
//
// One object holds the four things every screen needs -- the translator, the
// session, the router and what the server says about itself -- and it is passed
// down explicitly rather than reached for as a global. A page that takes its
// app as an argument can be rendered in German with an administrator logged in
// without any global state being true at the time.

import { Session, type VersionInfo } from "./api";
import { LANGUAGE_COOKIE, writeCookie } from "./cookies";
import { hasLanguage, resolveLanguage, Translator } from "./i18n";
import { closeAllModals } from "./components/modal";
import { renderShell } from "./chrome/shell";
import { Router, type Route } from "./router";
import { currentTheme, initTheme, toggleTheme, type Theme } from "./theme";

export class App {
  /** The translator for the current interface language. */
  t: Translator;

  /** What `/version` reported, or null when it has not answered. */
  version: VersionInfo | null = null;

  readonly session = new Session();
  readonly router: Router;

  private outlet: HTMLElement | null = null;
  private liveRegion: HTMLElement | null = null;

  constructor(
    private readonly root: HTMLElement,
    language: string,
    routes: (app: App) => Route[],
    notFound: (app: App) => Node,
  ) {
    this.t = new Translator(language);
    this.router = new Router(routes(this), () => notFound(this));
  }

  /** The interface language in force. */
  get language(): string {
    return this.t.language;
  }

  /** The theme in force. */
  get theme(): Theme {
    return currentTheme();
  }

  /** Builds the chrome and hands the router its outlet. */
  render(): void {
    const shell = renderShell(this);
    this.outlet = shell.outlet;
    this.liveRegion = shell.liveRegion;
    this.root.replaceChildren(shell.element);
    this.router.mount(shell.outlet);

    // The document's own language and direction follow the interface, so the
    // browser hyphenates, spell-checks and speaks the page correctly.
    document.documentElement.lang = this.language;
    document.documentElement.dir = this.t.meta.dir;
  }

  /**
   * Switches language.
   *
   * Writes the cookie and re-renders in place: docs/07_i18n.md says explicitly
   * that choosing a language must not reload the page.
   */
  setLanguage(code: string): void {
    if (!hasLanguage(code) || code === this.language) {
      return;
    }
    writeCookie(LANGUAGE_COOKIE, code);
    this.t = new Translator(code);
    closeAllModals();
    this.render();
    this.router.refresh();
  }

  /** Switches theme and re-renders, so the icon and its label agree. */
  switchTheme(): void {
    toggleTheme();
    this.render();
    this.router.refresh();
  }

  /** Navigates within the application. */
  navigate(path: string, options: { replace?: boolean } = {}): void {
    closeAllModals();
    this.router.navigate(path, options);
  }

  /**
   * Announces something to a screen reader without moving focus.
   *
   * Used for the changes that arrive on their own -- somebody else's item
   * appearing in an order -- which are invisible to a person who cannot see the
   * list change under them.
   */
  announce(message: string): void {
    if (!this.liveRegion) {
      return;
    }
    // Clearing first makes a repeated identical message announce again, which
    // matters when two people add the same thing.
    this.liveRegion.textContent = "";
    this.liveRegion.textContent = message;
  }

  /** Where pages are rendered. Null until render() has run. */
  get pageOutlet(): HTMLElement | null {
    return this.outlet;
  }
}

/**
 * Works out the starting language and theme, then builds the application.
 *
 * The language rule is the one in docs/07_i18n.md: the cookie, then the
 * browser's preferences, then English -- each step checked against the
 * catalogs actually in this build.
 */
export function createApp(
  root: HTMLElement,
  cookies: { language: string | null; theme: Theme | null },
  acceptLanguages: readonly string[],
  routes: (app: App) => Route[],
  notFound: (app: App) => Node,
): App {
  initTheme(cookies.theme);
  return new App(root, resolveLanguage(cookies.language, acceptLanguages), routes, notFound);
}
