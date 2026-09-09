// The API client.
//
// One place that knows how to talk to the backend: the base path, the CSRF
// header, the error envelope from docs/04_api.md and the shape of a session.
// Everything else in the frontend calls through here, so a change to any of
// those is a change to one file.

import { CSRF_COOKIE, readCookie } from "./cookies";
import type { Translator } from "./i18n";

/** Every endpoint lives below this. The version is part of the path. */
export const BASE_PATH = "/api/v1";

/** The error envelope. */
interface ErrorEnvelope {
  error?: {
    code?: number;
    message?: string;
    field?: string | null;
    request_id?: string;
  };
}

/**
 * The code used when the request never reached the server.
 *
 * Zero is outside every documented range, so it cannot collide with a real one,
 * and it has its own catalog key: "The server could not be reached" is a
 * different problem from anything the server itself would say.
 */
export const NETWORK_ERROR = 0;

/** A failed request, whether the server refused it or never answered. */
export class ApiError extends Error {
  /** The internal error number from docs/04_api.md, or NETWORK_ERROR. */
  readonly code: number;
  /** The HTTP status, or 0 when there was no response. */
  readonly status: number;
  /** The offending request field, for validation errors. */
  readonly field: string | null;
  /** The correlation id, which matches the server's log line. */
  readonly requestId: string;

  constructor(options: {
    code: number;
    status: number;
    message: string;
    field?: string | null;
    requestId?: string;
  }) {
    super(options.message);
    this.name = "ApiError";
    this.code = options.code;
    this.status = options.status;
    this.field = options.field ?? null;
    this.requestId = options.requestId ?? "";
  }

  /** Whether this error means "log in and try again". */
  get isAuthentication(): boolean {
    return this.code >= 2000 && this.code < 3000;
  }
}

/** Options for a request. Body is serialised as JSON unless it is FormData. */
export interface RequestOptions {
  body?: unknown;
  signal?: AbortSignal;
  headers?: Record<string, string>;
}

/**
 * Performs a request and returns the parsed body.
 *
 * A 204 returns undefined, which is why the type parameter defaults to void:
 * `await api.delete(path)` should not have to pretend it received something.
 */
export async function request<T = void>(
  method: string,
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json", ...options.headers };
  let body: BodyInit | undefined;

  if (options.body instanceof FormData) {
    // No Content-Type: the browser has to add the multipart boundary itself.
    body = options.body;
  } else if (options.body !== undefined) {
    headers["Content-Type"] = "application/json; charset=utf-8";
    body = JSON.stringify(options.body);
  }

  // The CSRF token is a double-submit: the server sets a readable cookie and
  // compares it against this header. Safe methods carry no ambient authority
  // to abuse, so they are exempt, exactly as the server's check is.
  if (!isSafe(method)) {
    const token = readCookie(CSRF_COOKIE);
    if (token) {
      headers["X-CSRF-Token"] = token;
    }
  }

  let response: Response;
  try {
    response = await fetch(BASE_PATH + path, {
      method,
      headers,
      body,
      // The session cookie is HttpOnly and SameSite=Strict; same-origin is
      // what sends it, and it is also what keeps this from ever being usable
      // cross-site.
      credentials: "same-origin",
      ...(options.signal ? { signal: options.signal } : {}),
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === "AbortError") {
      throw cause;
    }
    throw new ApiError({
      code: NETWORK_ERROR,
      status: 0,
      message: cause instanceof Error ? cause.message : "the request failed",
    });
  }

  if (!response.ok) {
    throw await errorFrom(response);
  }
  if (response.status === 204 || response.headers.get("Content-Length") === "0") {
    return undefined as T;
  }
  return (await response.json()) as T;
}

function isSafe(method: string): boolean {
  return method === "GET" || method === "HEAD" || method === "OPTIONS";
}

/**
 * Turns a failed response into an ApiError.
 *
 * A body that is not the documented envelope -- a proxy's own 502 page, say --
 * still has to produce something, so the status is mapped onto the nearest
 * internal code rather than throwing a second error while handling the first.
 */
async function errorFrom(response: Response): Promise<ApiError> {
  let envelope: ErrorEnvelope = {};
  try {
    envelope = (await response.json()) as ErrorEnvelope;
  } catch {
    envelope = {};
  }

  const error = envelope.error;
  return new ApiError({
    code: typeof error?.code === "number" ? error.code : fallbackCode(response.status),
    status: response.status,
    message: error?.message ?? response.statusText,
    field: error?.field ?? null,
    requestId: error?.request_id ?? response.headers.get("X-Request-Id") ?? "",
  });
}

function fallbackCode(status: number): number {
  if (status === 401) {
    return 2000;
  }
  if (status === 403) {
    return 3000;
  }
  if (status === 404) {
    return 4000;
  }
  if (status === 503) {
    return 9001;
  }
  return 9000;
}

/**
 * Reads or writes an endpoint whose body is HTML rather than JSON.
 *
 * Only the content pages: what they hold is a fragment of a document, and
 * wrapping it in a JSON string would only mean unwrapping it again. The
 * response is still an error envelope when something goes wrong, so failures
 * are reported the same way as everywhere else.
 */
async function requestText(method: string, path: string, html?: string): Promise<string> {
  const headers: Record<string, string> = { Accept: "text/html" };
  if (html !== undefined) {
    headers["Content-Type"] = "text/html; charset=utf-8";
  }
  if (!isSafe(method)) {
    const token = readCookie(CSRF_COOKIE);
    if (token) {
      headers["X-CSRF-Token"] = token;
    }
  }

  let response: Response;
  try {
    response = await fetch(BASE_PATH + path, {
      method,
      headers,
      credentials: "same-origin",
      ...(html === undefined ? {} : { body: html }),
    });
  } catch (cause) {
    throw new ApiError({
      code: NETWORK_ERROR,
      status: 0,
      message: cause instanceof Error ? cause.message : "the request failed",
    });
  }

  if (!response.ok) {
    throw await errorFrom(response);
  }
  return response.text();
}

/** The verbs, so call sites read as HTTP rather than as strings. */
export const api = {
  get: <T>(path: string, options?: RequestOptions) => request<T>("GET", path, options),
  /** Reads an HTML endpoint: the imprint and the legal notes. */
  text: (path: string) => requestText("GET", path),
  /** Replaces an HTML endpoint's content and returns what was stored. */
  putText: (path: string, html: string) => requestText("PUT", path, html),
  post: <T = void>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("POST", path, { ...options, ...(body === undefined ? {} : { body }) }),
  put: <T = void>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("PUT", path, { ...options, ...(body === undefined ? {} : { body }) }),
  patch: <T = void>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>("PATCH", path, { ...options, ...(body === undefined ? {} : { body }) }),
  delete: <T = void>(path: string, options?: RequestOptions) =>
    request<T>("DELETE", path, options),
};

/**
 * Reads a collection endpoint.
 *
 * Collections are not sent as bare arrays: each one is an object with a single
 * plural key holding the array -- `{"restaurants": [...]}`, `{"menu_items":
 * [...]}`. Unwrapping it in one place means the page code works in lists, and
 * means the one thing that has to know the key is the call that names the path
 * beside it.
 *
 * A missing key gives an empty list rather than an exception. A collection that
 * is not there and a collection that is empty are the same thing to a page
 * rendering it.
 */
export async function getList<T>(
  path: string,
  key: string,
  options?: RequestOptions,
): Promise<T[]> {
  const body = await request<Record<string, T[] | undefined>>("GET", path, options);
  return body[key] ?? [];
}

/**
 * The message to show a person for a failed request.
 *
 * The catalog is keyed on the internal code. The API's own `message` is English
 * developer-facing text and is only used when no translation exists, which
 * docs/07_i18n.md calls for and which means a new error number is legible in
 * production before its translation is written.
 */
export function errorMessage(t: Translator, error: unknown): string {
  if (!(error instanceof ApiError)) {
    return t.t("error.unknown");
  }
  if (error.code === NETWORK_ERROR) {
    return t.t("error.network");
  }
  const key = `error.${error.code}`;
  if (t.has(key)) {
    return t.t(key);
  }
  return error.message || t.t("error.unknown");
}

/** The current user, as `/auth/session` reports them. */
export interface SessionUser {
  id: string;
  name: string;
  display_name: string;
  is_admin: boolean;
}

interface SessionBody {
  user: SessionUser | null;
  expires_at?: string;
  /**
   * Present when the browser arrived holding a session the server would not
   * accept. Reported exactly once: the server clears the note when it hands it
   * over, so a later page view is plain anonymity.
   */
  ended?: { code: number; message: string };
}

/** What `/version` reports. */
export interface VersionInfo {
  version: string;
  commit: string;
  build_date: string;
  /** Whether Swagger UI is served. False under --no-swagger. */
  swagger: boolean;
  /** The largest image the server accepts, in bytes: --max-image-size. */
  max_image_size: number;
}

/**
 * The session, held in one place and observable.
 *
 * The title bar, the dropdown menu and every page need to know who is logged
 * in, and they must not each ask. Logging in or out updates this and everything
 * subscribed re-renders.
 */
export class Session {
  private current: SessionUser | null = null;
  private readonly listeners = new Set<(user: SessionUser | null) => void>();

  /** The logged-in user, or null. */
  get user(): SessionUser | null {
    return this.current;
  }

  get isAuthenticated(): boolean {
    return this.current !== null;
  }

  get isAdmin(): boolean {
    return this.current?.is_admin === true;
  }

  /**
   * Why the last session ended, if the server said so, and only once.
   *
   * Read by the entry point after the first load: somebody whose session
   * lapsed overnight is shown a logged-out page they did not ask for, and this
   * is what lets the application say why instead of leaving them to guess.
   */
  private ended: { code: number; message: string } | null = null;

  /** Asks the server who we are. Anonymous is an answer, not a failure. */
  async load(): Promise<SessionUser | null> {
    try {
      const body = await api.get<SessionBody>("/auth/session");
      this.ended = body.ended ?? null;
      this.set(body.user ?? null);
    } catch {
      // An unreachable server is not evidence of being logged out, but there
      // is nothing else to assume, and every write will fail visibly anyway.
      this.set(null);
    }
    return this.current;
  }

  /**
   * Takes the "your session ended" notice, if there is one, and forgets it.
   *
   * Taking rather than reading, so that it cannot be shown twice by two
   * callers who both wanted to be helpful.
   */
  takeEndedNotice(): { code: number; message: string } | null {
    const ended = this.ended;
    this.ended = null;
    return ended;
  }

  /** Ends the session. */
  async logout(): Promise<void> {
    try {
      await api.post("/auth/logout");
    } finally {
      this.set(null);
    }
  }

  /** Records a user after a login or a profile change. */
  set(user: SessionUser | null): void {
    this.current = user;
    for (const listener of this.listeners) {
      listener(user);
    }
  }

  /** Subscribes to changes. Returns the unsubscribe function. */
  subscribe(listener: (user: SessionUser | null) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }
}
