// Test helpers: a fake server and a mounted application.
//
// The page tests render real pages against a stubbed fetch rather than against
// stubbed page internals, so what they assert is what a browser would show.

import { vi } from "vitest";

import { createApp, type App } from "../src/app";
import type { Route } from "../src/router";
import { resetReferenceData } from "../src/reference";

/**
 * One canned answer with a status of its own.
 *
 * Marked rather than recognised by shape. The first version of this helper
 * treated any value with a `status` field as a canned answer, which worked
 * until an order body arrived: an order has a status, "active" or "expired",
 * and every order stub was quietly turned into an HTTP failure.
 */
export interface Stub {
  __stub: true;
  status: number;
  body?: unknown;
}

/** A stubbed failure: `fails(409, { error: { code: 4001 } })`. */
export function fails(status: number, body?: unknown): Stub {
  return { __stub: true, status, ...(body === undefined ? {} : { body }) };
}

function isStub(value: unknown): value is Stub {
  return typeof value === "object" && value !== null && "__stub" in value;
}

/**
 * A fetch stub keyed by "METHOD /path", with the /api/v1 prefix removed.
 *
 * A value that has a `status` is treated as a Stub -- a status and a body --
 * and anything else is the 200 body itself, which is what most routes want.
 */
export function stubServer(routes: Record<string, unknown>): {
  calls: { method: string; path: string; body: unknown }[];
} {
  const calls: { method: string; path: string; body: unknown }[] = [];

  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    const method = init?.method ?? "GET";
    const path = input.replace("/api/v1", "");
    calls.push({
      method,
      path,
      body: typeof init?.body === "string" ? (JSON.parse(init.body) as unknown) : init?.body,
    });

    const entry = routes[`${method} ${path}`];
    if (entry === undefined) {
      return Promise.resolve(response(404, { error: { code: 4000, message: "not stubbed" } }));
    }
    if (isStub(entry)) {
      return Promise.resolve(response(entry.status, entry.body ?? null));
    }
    return Promise.resolve(response(200, entry));
  });

  vi.stubGlobal("fetch", fetchMock);
  return { calls };
}

function response(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: `status ${status}`,
    headers: { get: () => null },
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

/** Mounts an application with the given routes, in English, dark. */
export function mountApp(routes: (app: App) => Route[]): App {
  resetReferenceData();

  const root = document.createElement("div");
  root.id = "app";
  document.body.appendChild(root);

  const app = createApp(root, { language: "en", theme: null }, ["en"], routes, () =>
    document.createElement("p"),
  );
  app.version = {
    version: "0.0.0-test",
    commit: "test",
    build_date: "2026-01-01T00:00:00Z",
    swagger: true,
    max_image_size: 5 * 1024 * 1024,
  };
  app.render();
  return app;
}

/** Lets every pending promise settle, so an async page has rendered. */
export async function settle(times = 6): Promise<void> {
  for (let index = 0; index < times; index++) {
    await Promise.resolve();
  }
}

/** The reference tables, as the five endpoints answer them. */
export const referenceStubs = {
  "GET /currencies": { currencies: [
    { code: "EUR", symbol: "€", minor_unit: 2, sort_order: 10 },
    { code: "CHF", symbol: "CHF", minor_unit: 2, sort_order: 20 },
  ] },
  "GET /contact-types": { contact_types: [
    { id: "ct-phone", code: "phone", render_as: "tel", sort_order: 10 },
    { id: "ct-email", code: "email", render_as: "mailto", sort_order: 40 },
  ] },
  "GET /tags": { tags: [{ id: "tag-vegan", code: "vegan", name: "vegan", sort_order: 10 }] },
  "GET /allergens": { allergens: [{ id: "al-gluten", code: "gluten", reference: "1", sort_order: 1 }] },
  "GET /additives": { additives: [{ id: "ad-colouring", code: "colouring", reference: "1", sort_order: 1 }] },
};
