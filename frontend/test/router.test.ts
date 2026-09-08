// The client-side router.

import { afterEach, describe, expect, it } from "vitest";

import { matchPath, Router, type Route } from "../src/router";

const routers: Router[] = [];

function build(routes: Route[]): { router: Router; outlet: HTMLElement } {
  const outlet = document.createElement("main");
  outlet.tabIndex = -1;
  document.body.appendChild(outlet);

  const router = new Router(routes, () => text("not found"));
  router.mount(outlet);
  routers.push(router);
  return { router, outlet };
}

function text(value: string): HTMLElement {
  const element = document.createElement("p");
  element.textContent = value;
  return element;
}

afterEach(() => {
  for (const router of routers.splice(0)) {
    router.stop();
  }
  document.body.replaceChildren();
  history.replaceState(null, "", "/");
});

describe("matching a path", () => {
  it("matches a literal path", () => {
    expect(matchPath("/restaurants", "/restaurants")).toEqual({});
    expect(matchPath("/restaurants", "/orders")).toBeNull();
  });

  it("captures parameters and decodes them", () => {
    expect(matchPath("/orders/:id", "/orders/018f-7c31")).toEqual({ id: "018f-7c31" });
    expect(matchPath("/orders/:id/summary", "/orders/7/summary")).toEqual({ id: "7" });
    expect(matchPath("/tags/:name", "/tags/extra%20scharf")).toEqual({ name: "extra scharf" });
  });

  it("does not match a different number of segments", () => {
    expect(matchPath("/orders/:id", "/orders")).toBeNull();
    expect(matchPath("/orders/:id", "/orders/7/summary")).toBeNull();
  });

  it("treats a trailing slash as the same path", () => {
    expect(matchPath("/restaurants", "/restaurants/")).toEqual({});
  });
});

describe("the router", () => {
  it("renders the matching route", async () => {
    history.replaceState(null, "", "/orders/42");
    const { router, outlet } = build([
      { pattern: "/orders/:id", render: (context) => text(`order ${context.params["id"]}`) },
    ]);

    router.start();
    await Promise.resolve();
    expect(outlet.textContent).toBe("order 42");
  });

  it("renders the not-found page for a path nothing matches", async () => {
    history.replaceState(null, "", "/nowhere");
    const { router, outlet } = build([{ pattern: "/", render: () => text("orders") }]);

    router.start();
    await Promise.resolve();
    expect(outlet.textContent).toBe("not found");
  });

  it("navigates and pushes history", async () => {
    const { router, outlet } = build([
      { pattern: "/", render: () => text("orders") },
      { pattern: "/restaurants", render: () => text("restaurants") },
    ]);
    router.start();
    await Promise.resolve();

    router.navigate("/restaurants");
    await Promise.resolve();
    expect(outlet.textContent).toBe("restaurants");
    expect(location.pathname).toBe("/restaurants");
  });

  it("turns a click on an internal link into a navigation", async () => {
    const { router, outlet } = build([
      { pattern: "/", render: () => text("orders") },
      { pattern: "/restaurants", render: () => text("restaurants") },
    ]);
    router.start();
    await Promise.resolve();

    const link = document.createElement("a");
    link.href = "/restaurants";
    document.body.appendChild(link);
    link.click();
    await Promise.resolve();

    expect(outlet.textContent).toBe("restaurants");
  });

  it("leaves the paths the server owns alone", async () => {
    const { router, outlet } = build([{ pattern: "/", render: () => text("orders") }]);
    router.start();
    await Promise.resolve();

    // Swagger UI is a page the server renders. Intercepting the link would
    // look for a route that does not exist and show "not found".
    const link = document.createElement("a");
    link.href = "/tools/swagger/";
    document.body.appendChild(link);

    // Recorded after the router has had the event and before jsdom acts on it:
    // letting a real navigation through would only produce a "not implemented"
    // from a headless browser that cannot navigate anywhere.
    let prevented: boolean | null = null;
    document.addEventListener("click", (event) => {
      prevented = event.defaultPrevented;
      event.preventDefault();
    });

    link.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 }));
    await Promise.resolve();

    expect(prevented).toBe(false);
    expect(outlet.textContent).toBe("orders");
  });

  it("does not undo the page it is installing", async () => {
    // The defect a browser found and no unit test had: the cleanups were kept
    // on the router, so a page registering one while it was being built had it
    // run by the very render that was building it. The order page opened its
    // event stream and the router closed it a moment later, and the page then
    // sat there showing data that never changed.
    const closed: string[] = [];
    const { router, outlet } = build([
      {
        pattern: "/",
        render: (context) => {
          context.onCleanup(() => closed.push("first"));
          return text("first");
        },
      },
      {
        pattern: "/second",
        render: (context) => {
          context.onCleanup(() => closed.push("second"));
          return text("second");
        },
      },
    ]);

    router.start();
    await Promise.resolve();
    expect(outlet.textContent).toBe("first");
    expect(closed).toEqual([]);

    router.navigate("/second");
    await Promise.resolve();
    // Only the outgoing page is undone.
    expect(closed).toEqual(["first"]);

    router.stop();
    expect(closed).toEqual(["first", "second"]);
  });

  it("undoes a page that was overtaken before it was ever shown", async () => {
    const closed: string[] = [];
    let release: () => void = () => {};

    const { router, outlet } = build([
      { pattern: "/", render: () => text("orders") },
      {
        pattern: "/slow",
        render: async (context) => {
          context.onCleanup(() => closed.push("slow"));
          await new Promise<void>((resolve) => {
            release = resolve;
          });
          return text("slow");
        },
      },
      { pattern: "/fast", render: () => text("fast") },
    ]);
    router.start();
    await Promise.resolve();

    router.navigate("/slow");
    await Promise.resolve();
    router.navigate("/fast");
    await Promise.resolve();

    release();
    await Promise.resolve();
    await Promise.resolve();

    expect(outlet.textContent).toBe("fast");
    // The abandoned page opened something; it has to be closed even though the
    // page was never shown.
    expect(closed).toEqual(["slow"]);
  });

  it("shows the newest navigation when two renders overlap", async () => {
    let release: () => void = () => {};
    const { router, outlet } = build([
      {
        pattern: "/slow",
        render: async () => {
          await new Promise<void>((resolve) => {
            release = resolve;
          });
          return text("slow");
        },
      },
      { pattern: "/fast", render: () => text("fast") },
    ]);
    router.start();
    await Promise.resolve();

    router.navigate("/slow");
    await Promise.resolve();
    router.navigate("/fast");
    await Promise.resolve();

    // The slow page finishes last and must not overwrite what replaced it.
    release();
    await Promise.resolve();
    await Promise.resolve();
    expect(outlet.textContent).toBe("fast");
  });
});
