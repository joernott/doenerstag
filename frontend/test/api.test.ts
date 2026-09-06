// The API client.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, ApiError, errorMessage, NETWORK_ERROR, request, Session } from "../src/api";
import { Translator } from "../src/i18n";

/**
 * A stand-in for what fetch resolves to.
 *
 * Hand-built rather than a real Response: only the four members the client
 * touches are needed, and a fake that is exactly those four cannot quietly
 * depend on behaviour the client does not actually use.
 */
function response(status: number, body: string, headers: Record<string, string> = {}): Response {
  const lookup = new Map(Object.entries(headers).map(([name, value]) => [name.toLowerCase(), value]));
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: `status ${status}`,
    headers: { get: (name: string) => lookup.get(name.toLowerCase()) ?? null },
    json: () => Promise.resolve(JSON.parse(body) as unknown),
  } as unknown as Response;
}

/** A JSON response. */
function json(status: number, body: unknown): Response {
  return response(status, JSON.stringify(body), { "Content-Type": "application/json" });
}

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  document.cookie = "doener_csrf=; max-age=0; path=/";
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** The options fetch was called with, for asserting on headers. */
function lastRequest(): { url: string; init: RequestInit & { headers: Record<string, string> } } {
  const call = fetchMock.mock.calls[0] as [string, RequestInit & { headers: Record<string, string> }];
  return { url: call[0], init: call[1] };
}

describe("requests", () => {
  it("puts everything below the versioned base path", async () => {
    fetchMock.mockResolvedValue(json(200, { user: null }));
    await api.get("/auth/session");
    expect(lastRequest().url).toBe("/api/v1/auth/session");
  });

  it("sends the CSRF token from the cookie on a write", async () => {
    document.cookie = "doener_csrf=token-value; path=/";
    fetchMock.mockResolvedValue(json(201, { id: "1" }));

    await api.post("/restaurants", { name: "Pinar" });

    const { init } = lastRequest();
    expect(init.headers["X-CSRF-Token"]).toBe("token-value");
    expect(init.body).toBe(JSON.stringify({ name: "Pinar" }));
  });

  it("does not send it on a read", async () => {
    document.cookie = "doener_csrf=token-value; path=/";
    fetchMock.mockResolvedValue(json(200, []));

    await api.get("/restaurants");
    expect(lastRequest().init.headers["X-CSRF-Token"]).toBeUndefined();
  });

  it("returns nothing for a 204", async () => {
    fetchMock.mockResolvedValue(response(204, ""));
    await expect(api.delete("/restaurants/1")).resolves.toBeUndefined();
  });

  it("reads the error envelope", async () => {
    fetchMock.mockResolvedValue(
      json(400, {
        error: {
          code: 1003,
          message: "deadline must be before the fulfilment time",
          field: "deadline_at",
          request_id: "01932f3c",
        },
      }),
    );

    const error = await api.post("/orders", {}).catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      code: 1003,
      status: 400,
      field: "deadline_at",
      requestId: "01932f3c",
    });
  });

  it("recognises an authentication failure by its range", async () => {
    fetchMock.mockResolvedValue(json(401, { error: { code: 2002, message: "session expired" } }));
    const error = (await api.get("/orders").catch((cause: unknown) => cause)) as ApiError;
    expect(error.isAuthentication).toBe(true);
  });

  it("still produces an error when the body is not ours", async () => {
    // A proxy's own 503 page, say. Something has to be thrown, and it has to
    // carry a code the interface can translate.
    fetchMock.mockResolvedValue(response(503, "<html>gateway</html>", { "Content-Type": "text/html" }));
    const error = (await api.get("/orders").catch((cause: unknown) => cause)) as ApiError;
    expect(error.code).toBe(9001);
  });

  it("reports a request that never arrived", async () => {
    fetchMock.mockRejectedValue(new TypeError("failed to fetch"));
    const error = (await api.get("/orders").catch((cause: unknown) => cause)) as ApiError;
    expect(error.code).toBe(NETWORK_ERROR);
    expect(error.status).toBe(0);
  });

  it("passes an abort straight through", async () => {
    const abort = new DOMException("aborted", "AbortError");
    fetchMock.mockRejectedValue(abort);
    await expect(request("GET", "/orders")).rejects.toBe(abort);
  });
});

describe("error messages", () => {
  const t = new Translator("de");

  it("translates a known code", () => {
    const error = new ApiError({ code: 4001, status: 409, message: "order is read-only" });
    expect(errorMessage(t, error)).toBe(t.t("error.4001"));
  });

  it("falls back to the API's English message for a code with no translation", () => {
    const error = new ApiError({ code: 7777, status: 400, message: "something new" });
    expect(errorMessage(t, error)).toBe("something new");
  });

  it("has its own wording for an unreachable server", () => {
    const error = new ApiError({ code: NETWORK_ERROR, status: 0, message: "failed to fetch" });
    expect(errorMessage(t, error)).toBe(t.t("error.network"));
  });

  it("says something for a failure that is not an ApiError at all", () => {
    expect(errorMessage(t, new Error("boom"))).toBe(t.t("error.unknown"));
  });
});

describe("the session", () => {
  it("records who is logged in and tells its subscribers", async () => {
    const user = { id: "1", name: "root", display_name: "Root", is_admin: true };
    fetchMock.mockResolvedValue(json(200, { user }));

    const session = new Session();
    const seen: (typeof user | null)[] = [];
    session.subscribe((current) => seen.push(current));

    await session.load();
    expect(session.user).toEqual(user);
    expect(session.isAdmin).toBe(true);
    expect(seen).toEqual([user]);
  });

  it("treats an unreachable server as anonymous", async () => {
    fetchMock.mockRejectedValue(new TypeError("failed to fetch"));
    const session = new Session();
    await session.load();
    expect(session.isAuthenticated).toBe(false);
  });

  it("clears the user even when the logout call fails", async () => {
    fetchMock.mockResolvedValueOnce(
      json(200, { user: { id: "1", name: "root", display_name: "Root", is_admin: false } }),
    );
    const session = new Session();
    await session.load();

    fetchMock.mockRejectedValueOnce(new TypeError("failed to fetch"));
    await session.logout().catch(() => undefined);
    expect(session.user).toBeNull();
  });
});
