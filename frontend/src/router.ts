// The client-side router.
//
// The server answers every unknown path with index.html (the SPA fallback in
// docs/08_technologies.md), so real URLs work: a pasted link to an order opens
// that order, and the back button behaves. Paths the server owns -- the API,
// the static assets, Swagger UI -- are deliberately not intercepted, so a link
// to /tools/swagger leaves the application rather than looking for a route that
// does not exist.

/** What a page render is given. */
export interface RouteContext {
  /** The path that matched, without the query string. */
  path: string;
  /** The `:name` parameters from the pattern, already decoded. */
  params: Record<string, string>;
  /** The query string. */
  query: URLSearchParams;
}

/** A page. */
export interface Route {
  /** A path pattern, e.g. `/orders/:id/summary`. */
  pattern: string;
  /** Builds the page. May be asynchronous; the outlet waits for it. */
  render: (context: RouteContext) => Node | Promise<Node>;
}

/** Paths the server serves itself and the router must not swallow. */
const serverPaths = ["/api/", "/static/", "/tools/", "/health", "/metrics"];

/**
 * Matches one pattern against one path.
 *
 * Exported for the tests, and because the matching rule -- literal segments and
 * `:name` parameters, no wildcards, no optional segments -- is small enough
 * that its behaviour is easier to assert than to argue about.
 */
export function matchPath(
  pattern: string,
  path: string,
): Record<string, string> | null {
  const expected = segments(pattern);
  const actual = segments(path);
  if (expected.length !== actual.length) {
    return null;
  }

  const params: Record<string, string> = {};
  for (const [index, part] of expected.entries()) {
    const value = actual[index] ?? "";
    if (part.startsWith(":")) {
      if (value === "") {
        return null;
      }
      params[part.slice(1)] = decodeURIComponent(value);
      continue;
    }
    if (part !== value) {
      return null;
    }
  }
  return params;
}

function segments(path: string): string[] {
  return path.split("/").filter((part) => part !== "");
}

/** The router. */
export class Router {
  private outlet: HTMLElement | null = null;
  private listeners = new Set<(context: RouteContext) => void>();

  // Renders are asynchronous, so two navigations in quick succession can
  // finish out of order. Each render takes a ticket and only the newest one is
  // allowed to touch the DOM.
  private ticket = 0;

  constructor(
    private readonly routes: Route[],
    private readonly notFound: (context: RouteContext) => Node | Promise<Node>,
  ) {}

  /** Attaches the router to the element pages are rendered into. */
  mount(outlet: HTMLElement): void {
    this.outlet = outlet;
  }

  /** Starts routing: intercepts links, listens for history changes, renders. */
  start(): void {
    document.addEventListener("click", this.onClick);
    window.addEventListener("popstate", this.onPopState);
    void this.render();
  }

  /** Detaches the listeners. Only a test needs this. */
  stop(): void {
    document.removeEventListener("click", this.onClick);
    window.removeEventListener("popstate", this.onPopState);
  }

  /** Navigates to a path within the application. */
  navigate(path: string, options: { replace?: boolean } = {}): void {
    if (path === current()) {
      return;
    }
    if (options.replace) {
      history.replaceState(null, "", path);
    } else {
      history.pushState(null, "", path);
    }
    void this.render();
  }

  /** Re-renders the current page, after a language or session change. */
  refresh(): void {
    void this.render();
  }

  /** Notified after every render, with the context that was rendered. */
  onRender(listener: (context: RouteContext) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** The context for the current URL, whether or not it matches a route. */
  context(): RouteContext {
    const url = new URL(location.href);
    for (const route of this.routes) {
      const params = matchPath(route.pattern, url.pathname);
      if (params) {
        return { path: url.pathname, params, query: url.searchParams };
      }
    }
    return { path: url.pathname, params: {}, query: url.searchParams };
  }

  private async render(): Promise<void> {
    const outlet = this.outlet;
    if (!outlet) {
      return;
    }

    const mine = ++this.ticket;
    const url = new URL(location.href);
    const context: RouteContext = {
      path: url.pathname,
      params: {},
      query: url.searchParams,
    };

    let build: (context: RouteContext) => Node | Promise<Node> = this.notFound;
    for (const route of this.routes) {
      const params = matchPath(route.pattern, url.pathname);
      if (params) {
        context.params = params;
        build = route.render;
        break;
      }
    }

    const page = await build(context);
    if (mine !== this.ticket) {
      return;
    }

    outlet.replaceChildren(page);

    // Focus moves to the top of the new page, or a keyboard user would carry
    // on from wherever the link they followed happened to be. tabindex="-1" on
    // the outlet is what makes that possible without adding a tab stop.
    outlet.focus({ preventScroll: true });
    window.scrollTo(0, 0);

    for (const listener of this.listeners) {
      listener(context);
    }
  }

  private readonly onPopState = (): void => {
    void this.render();
  };

  /**
   * Turns an ordinary link into a navigation.
   *
   * Modified clicks are left alone -- ctrl-click opens a new tab and must keep
   * doing so -- as are downloads, external targets and the paths the server
   * owns.
   */
  private readonly onClick = (event: MouseEvent): void => {
    if (event.defaultPrevented || event.button !== 0) {
      return;
    }
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }

    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const link = target.closest("a");
    if (!link || link.target || link.hasAttribute("download")) {
      return;
    }

    const href = link.getAttribute("href");
    if (!href || href.startsWith("#")) {
      return;
    }

    const url = new URL(href, location.href);
    if (url.origin !== location.origin) {
      return;
    }
    if (serverPaths.some((prefix) => url.pathname.startsWith(prefix))) {
      return;
    }

    event.preventDefault();
    this.navigate(url.pathname + url.search);
  };
}

/** The current path with its query string. */
function current(): string {
  return location.pathname + location.search;
}
