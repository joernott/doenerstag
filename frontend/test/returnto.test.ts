import { describe, expect, it } from "vitest";

import { RETURN_TO, loginHref, returnPathFrom } from "../src/returnto";

describe("the login link", () => {
  it("carries the page it was pressed on", () => {
    expect(loginHref("/orders/abc123")).toBe("/account?next=%2Forders%2Fabc123");
  });

  it("keeps a query string that was part of the page", () => {
    expect(loginHref("/restaurants?tag=vegan")).toBe(
      "/account?next=%2Frestaurants%3Ftag%3Dvegan",
    );
  });

  // Returning to the page somebody is already on is what happens without any
  // of this, so the parameter would be noise in the address bar.
  it("carries nothing when it was pressed on the account page", () => {
    expect(loginHref("/account")).toBe("/account");
    expect(loginHref("/account?next=/orders/1")).toBe("/account");
  });
});

describe("the return path", () => {
  it("is read back from the query", () => {
    const query = new URLSearchParams({ [RETURN_TO]: "/orders/abc123" });
    expect(returnPathFrom(query)).toBe("/orders/abc123");
  });

  it("is null when there is none", () => {
    expect(returnPathFrom(new URLSearchParams())).toBeNull();
  });

  /*
   * The open redirect, which is the reason this is validated rather than
   * trusted.
   *
   * A login page that navigates wherever a query parameter says lets somebody
   * send a colleague a link that logs them in and then puts them on a page of
   * the sender's choosing -- a copy of this application asking for the password
   * again, most usefully. The parameter is attacker-controlled by construction,
   * because it arrives in a URL somebody was sent.
   *
   * So anything that is not a path rooted at a single slash is discarded, and
   * the caller falls back to the account page.
   */
  it("refuses anything that leaves this site", () => {
    for (const hostile of [
      "https://evil.example/login",
      "http://evil.example",
      "//evil.example",
      "/\\evil.example",
      "\\\\evil.example",
      "javascript:alert(1)",
      "orders/abc",
      "",
    ]) {
      const query = new URLSearchParams({ [RETURN_TO]: hostile });
      expect(returnPathFrom(query), `should have refused ${JSON.stringify(hostile)}`).toBeNull();
    }
  });

  it("accepts an ordinary path", () => {
    for (const fine of ["/", "/orders", "/orders/abc123", "/orders/abc123/summary", "/r?a=b"]) {
      const query = new URLSearchParams({ [RETURN_TO]: fine });
      expect(returnPathFrom(query), `should have accepted ${fine}`).toBe(fine);
    }
  });
});
