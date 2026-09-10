# 04 API

## Conventions

- **Base path**: every endpoint is below `/api/v1`. The version is part of the
  path so that a future `/api/v2` can be served alongside.
- **Content type**: `application/json; charset=utf-8` for requests and
  responses, except image upload (`multipart/form-data`), image download
  (`image/*`), the SSE stream (`text/event-stream`) and content page HTML.
- **Encoding**: UTF-8 everywhere.
- **Timestamps**: RFC 3339 with a `Z` suffix. All times are UTC.
- **Identifiers**: UUIDv7 in canonical hyphenated lowercase form.
- **Money**: integers in the minor unit, always accompanied by the applicable
  `currency_code` in the enclosing object. Field names end in `_cents`.
- **Collections are wrapped.** A collection endpoint answers with an object
  carrying one plural key, never with a bare array: `GET /restaurants` returns
  `{"restaurants": [...]}`, `GET /restaurants/{id}/menu-items` returns
  `{"menu_items": [...]}`, and so on for `categories`, `contacts`,
  `opening_hours`, `modifications`, `currencies`, `contact_types`, `tags`,
  `allergens`, `additives`, `orders`, `users` and `tokens`. A top-level JSON
  array is a known hazard, and the envelope leaves room to add a field beside
  the list without changing the type of the response.
- **No pagination.** Collection endpoints return the complete collection. This
  is a deliberate decision given the expected scale (see
  [01_overview.md](01_overview.md) and
  [adr/0006-no-pagination.md](adr/0006-no-pagination.md)).
- **Filtering**: menu items can be filtered by tags, allergens and additives.
  Restaurants and orders are not filterable.
- **Documentation**: the API is described by an OpenAPI 3.1 document at
  `/api/v1/openapi.json`. Swagger UI is served from `/tools/swagger` unless
  disabled with `--no-swagger`.
- **Routing**: all routes — API, static assets and the SPA fallback — are
  registered on a single `julienschmidt/httprouter` router. Path parameters use
  httprouter's `:name` syntax, so `/orders/{id}/items/{iid}` in this document is
  `/orders/:id/items/:iid` in code. See
  [08_technologies.md](08_technologies.md) for the routing rules that follow
  from that choice.

### HTTP methods

| Method   | Use                                                            |
| -------- | -------------------------------------------------------------- |
| `GET`    | Read. Never changes state. Safe for anonymous callers.          |
| `POST`   | Create a subordinate resource, or invoke an action.             |
| `PUT`    | Replace a resource in full.                                     |
| `PATCH`  | Partial update. Only fields present in the body are changed.    |
| `DELETE` | Remove a resource.                                              |

### Success status codes

| Code  | Use                                             |
| ----- | ----------------------------------------------- |
| `200` | Successful `GET`, `PUT`, `PATCH`, action `POST`. |
| `201` | Resource created. `Location` header is set.      |
| `204` | Successful `DELETE`.                             |

## Error responses

Errors use the matching HTTP status code and carry a JSON body:

```json
{
  "error": {
    "code": 1003,
    "message": "deadline must be before the fulfilment time",
    "field": "deadline_at",
    "request_id": "01932f3c-7b2a-7c31-9a44-2f0f9e6b1c77"
  }
}
```

| Field        | Null | Meaning                                                            |
| ------------ | ---- | ------------------------------------------------------------------ |
| `code`       | no   | Internal, stable error number. See the table below.                 |
| `message`    | no   | Short, human-readable English message. Not for display to end users.|
| `field`      | yes  | The offending request field, for validation errors.                 |
| `request_id` | no   | Correlation ID. Matches the `X-Request-Id` header and the log line. |

The frontend translates errors through its i18n catalog keyed on `code` and
falls back to `message` when no translation exists.

### Error number ranges

| Range       | Meaning                | Typical HTTP status  |
| ----------- | ---------------------- | -------------------- |
| 1000 – 1999 | Request validation     | 400, 413, 415        |
| 2000 – 2999 | Authentication         | 401                  |
| 3000 – 3999 | Authorization          | 403                  |
| 4000 – 4999 | Resource state         | 404, 405, 409, 410   |
| 5000 – 5999 | Rate limiting          | 429                  |
| 9000 – 9999 | Server / database      | 500, 503             |

### Defined error numbers

| Code | HTTP | Meaning                                                        |
| ---- | ---- | -------------------------------------------------------------- |
| 1000 | 400  | Malformed JSON body.                                            |
| 1001 | 400  | Required field missing.                                         |
| 1002 | 400  | Field value out of range or wrongly formatted.                  |
| 1003 | 400  | Deadline is not before the fulfilment time.                     |
| 1004 | 400  | Quantity must be at least 1.                                    |
| 1005 | 400  | Menu item does not belong to the order's restaurant.            |
| 1006 | 400  | Modification does not belong to the selected menu item.         |
| 1007 | 400  | Restaurant needs at least one contact entry.                    |
| 1008 | 400  | Opening hours entry has equal start and end time.               |
| 1009 | 400  | User name already taken.                                        |
| 1010 | 400  | Password does not meet the minimum requirements.                |
| 1011 | 413  | Uploaded image exceeds the configured maximum size.             |
| 1012 | 415  | Unsupported image media type.                                   |
| 1013 | 400  | Unknown currency code.                                          |
| 1014 | 400  | The deadline is already in the past.                            |
| 1015 | 400  | This password reset link is not valid: forged, mangled or already used. |
| 1016 | 400  | This password reset link has expired.                           |
| 2000 | 401  | Not authenticated.                                              |
| 2001 | 401  | Invalid user name or password.                                  |
| 2002 | 401  | Session expired (idle or absolute timeout).                     |
| 2003 | 401  | Session superseded by a newer login.                            |
| 2004 | 401  | Invalid or revoked API token.                                   |
| 2005 | 403  | Missing or invalid CSRF token.                                  |
| 2006 | 403  | Already logged in; log out before registering another account.  |
| 3000 | 403  | Administrator privileges required.                              |
| 3001 | 403  | Only the order creator may change this order.                   |
| 3002 | 403  | Only the owner may change this order item.                      |
| 3003 | 403  | The deleted-user placeholder cannot be modified.                |
| 3004 | 403  | Only participants of this order may see its summary.            |
| 3005 | 403  | Only the owner of this resource may act on it.                   |
| 4000 | 404  | Resource not found.                                             |
| 4001 | 409  | Order deadline has passed; the order is read-only.              |
| 4002 | 409  | The restaurant cannot be changed once the order has items.      |
| 4003 | 409  | The restaurant is still referenced by an order.                 |
| 4004 | 409  | Menu item is marked unavailable.                                |
| 4005 | 409  | Name already exists within this restaurant.                     |
| 4006 | 405  | The HTTP method is not allowed on this path.                    |
| 4007 | 409  | Somebody is already doing this job.                             |
| 4008 | 409  | The kitchen does not make this at the order's time.             |
| 5000 | 429  | Too many login attempts.                                        |
| 9000 | 500  | Unexpected server error.                                        |
| 9001 | 503  | Database unavailable.                                           |

## Authentication on the wire

Two mechanisms, described fully in
[05_auth_and_permissions.md](05_auth_and_permissions.md):

1. **Browser sessions** — a JWT in the `doener_session` cookie
   (`HttpOnly`, `Secure`, `SameSite=Strict`). State-changing requests must also
   send the CSRF token from the `doener_csrf` cookie in the `X-CSRF-Token`
   header.
2. **API tokens** — `Authorization: Bearer <token>`. Not subject to CSRF
   checks, because they carry no ambient browser credentials.

Anonymous `GET` requests are allowed on all read endpoints marked *public*
below.

---

## Endpoints

Legend for the *Access* column:

| Value       | Meaning                                                  |
| ----------- | -------------------------------------------------------- |
| public      | No authentication required. May still return less to an anonymous caller — see `GET /orders/{id}`. |
| participant | The order's creator, any user with at least one item in the order, or the administrator. |
| user        | Any authenticated user.                                   |
| owner       | The user who owns the resource, or the administrator.     |
| creator     | The user who created the order, or the administrator.     |
| admin       | The `root` user only.                                     |

### Authentication and accounts

| Method   | Path                       | Access | Description                                                      |
| -------- | -------------------------- | ------ | ---------------------------------------------------------------- |
| `POST`   | `/auth/register`           | public | Create an account. Returns the new user and starts a session.     |
| `POST`   | `/auth/login`              | public | Log in. Replaces any existing session for that user.              |
| `POST`   | `/auth/logout`             | user   | End the current session.                                          |
| `GET`    | `/auth/session`            | public | Current session info, or `{"user": null}` when anonymous. Adds `ended` when the caller arrived with a dead session cookie. |
| `GET`    | `/users`                   | admin  | List all users.                                                   |
| `GET`    | `/users/{id}`              | public | Public profile: id, name, display name.                           |
| `PATCH`  | `/users/{id}`              | owner  | Change name, display name, e-mail or password.                    |
| `DELETE` | `/users/{id}`              | owner  | Delete the account. See the impact rules in F2.6.                 |
| `GET`    | `/users/{id}/deletion-impact` | owner | How many active orders and items would be affected.             |
| `GET`    | `/users/{id}/tokens`       | owner  | List API tokens. Never returns the token value.                   |
| `POST`   | `/users/{id}/tokens`       | owner  | Create a token. Returns the value **once**.                       |
| `DELETE` | `/users/{id}/tokens/{tid}` | owner  | Revoke a token.                                                   |

The `ended` object on `/auth/session` is how a browser learns that the
session it thought it had is gone:

```json
{ "user": null, "ended": { "code": 2003, "message": "session superseded by a newer login" } }
```

It appears only alongside a null user, and only once -- reading it clears
the note, so the next page view is plain anonymity. A cookie that no longer
works never fails a request; see
[05_auth_and_permissions.md](05_auth_and_permissions.md) for why.

### Restaurants

| Method   | Path                                        | Access | Description                                    |
| -------- | ------------------------------------------- | ------ | ---------------------------------------------- |
| `GET`    | `/restaurants`                              | public | All restaurants, with logo image id.           |
| `POST`   | `/restaurants`                              | user   | Create a restaurant.                           |
| `GET`    | `/restaurants/{id}`                         | public | Restaurant with contacts and opening hours.    |
| `PATCH`  | `/restaurants/{id}`                         | user   | Update restaurant fields.                      |
| `DELETE` | `/restaurants/{id}`                         | admin  | Soft delete. 409 if referenced by an order.    |
| `GET`    | `/restaurants/{id}/contacts`                | public | Contact entries.                               |
| `POST`   | `/restaurants/{id}/contacts`                | user   | Add a contact entry.                           |
| `PUT`    | `/restaurants/{id}/contacts/{cid}`          | user   | Replace a contact entry.                       |
| `DELETE` | `/restaurants/{id}/contacts/{cid}`          | user   | Remove a contact entry. 400 if it is the last. |
| `GET`    | `/restaurants/{id}/opening-hours`           | public | Opening hours.                                 |
| `PUT`    | `/restaurants/{id}/opening-hours`           | user   | Replace the whole set in one call.             |

### Menu

| Method   | Path                                                  | Access | Description                                        |
| -------- | ----------------------------------------------------- | ------ | -------------------------------------------------- |
| `GET`    | `/restaurants/{id}/categories`                        | public | Categories, ordered by `sort_order`, `name`.       |
| `POST`   | `/restaurants/{id}/categories`                        | user   | Add a category.                                    |
| `PATCH`  | `/restaurants/{id}/categories/{cid}`                  | user   | Rename or reorder.                                 |
| `DELETE` | `/restaurants/{id}/categories/{cid}`                  | admin  | Soft delete.                                       |
| `GET`    | `/restaurants/{id}/menu-items`                        | public | Menu items. Supports the filters below.            |
| `POST`   | `/restaurants/{id}/menu-items`                        | user   | Add a menu item.                                   |
| `GET`    | `/restaurants/{id}/menu-items/{mid}`                  | public | One menu item with its modifications.              |
| `PATCH`  | `/restaurants/{id}/menu-items/{mid}`                  | user   | Update, including the `available` flag.            |
| `DELETE` | `/restaurants/{id}/menu-items/{mid}`                  | admin  | Soft delete.                                       |
| `GET`    | `/restaurants/{id}/menu-items/{mid}/modifications`    | public | Predefined modifications.                          |
| `POST`   | `/restaurants/{id}/menu-items/{mid}/modifications`    | user   | Add a modification.                                |
| `PATCH`  | `/restaurants/{id}/menu-items/{mid}/modifications/{k}`| user   | Update a modification.                             |
| `DELETE` | `/restaurants/{id}/menu-items/{mid}/modifications/{k}`| admin  | Soft delete a modification.                        |

Menu item query parameters, all repeatable and combined with AND across
parameter names, OR within one parameter name:

| Parameter        | Example                    | Meaning                                        |
| ---------------- | -------------------------- | ---------------------------------------------- |
| `category`       | `?category=<uuid>`         | Restrict to a category.                         |
| `tag`            | `?tag=vegan&tag=spicy`     | Item carries any of these tags.                 |
| `exclude_allergen`| `?exclude_allergen=nuts`  | Item does **not** carry this allergen.          |
| `exclude_additive`| `?exclude_additive=gmo`   | Item does **not** carry this additive.          |
| `available`      | `?available=true`          | Only orderable items.                           |

### Orders

| Method   | Path                             | Access  | Description                                           |
| -------- | -------------------------------- | ------- | ----------------------------------------------------- |
| `GET`    | `/orders`                        | public  | All orders, active first, each with derived title, status, deadline and item count. Never any item detail. |
| `POST`   | `/orders`                        | user    | Create an order. Copies currency, minimum value and delivery fee from the restaurant. |
| `GET`    | `/orders/{id}`                   | public  | Order header. Items included only for authenticated callers — see below. |
| `PATCH`  | `/orders/{id}`                   | creator | Update order fields. 409 after the deadline.          |
| `DELETE` | `/orders/{id}`                   | creator | Delete the order and everything below it.             |
| `GET`    | `/orders/{id}/summary`           | participant | Aggregated summary, see below. 403 error 3004 for everyone else. |
| `GET`    | `/orders/{id}/events`            | public  | SSE stream of changes to this order. Payloads depend on authentication. |
| `POST`   | `/orders/{id}/items`             | user    | Add an order item. Snapshots name and price.          |
| `PATCH`  | `/orders/{id}/items/{iid}`       | owner   | Change quantity, note or modifications.               |
| `DELETE` | `/orders/{id}/items/{iid}`       | owner   | Remove the item.                                      |

### Order visibility

`GET /orders/{id}` returns a different shape depending on who is asking (F1.2):

- **Anonymous caller** — the order header only: id, derived title, restaurant,
  fulfilment type and time, deadline, status, currency, minimum order value,
  delivery fee, and `item_count`. The `items` field is **absent**, no total of
  any kind is returned, and **no user is named** — the creator included.
- **Authenticated caller** — the same header plus the creator, plus `items`,
  each with its owner, quantity, snapshots, modifications and line total, plus
  the order totals.

`item_count` is present in both shapes so the frontend does not branch on it.
`GET /orders` behaves the same way for every caller: header and `item_count`
only, never item detail.

The distinction is enforced in the handler, not by the frontend hiding fields.
An anonymous caller must never receive item data in any response.

### Summary access

`GET /orders/{id}/summary` is restricted to participants (F1.3). A participant
is the order's creator, any user holding at least one `order_item` in the order,
or the administrator. Anonymous callers and logged-in non-participants get 403
with error 3004.

The creator counts as a participant even with no items of their own, because the
creator is usually the person who phones the restaurant and the summary is what
they read from.

The summary response contains:

```json
{
  "order_id": "…",
  "title": "Döner Palast — 2026-09-10 12:30",
  "currency_code": "EUR",
  "aggregated": [
    {
      "item_name": "Döner Kebab",
      "external_id": "12",
      "modifications": ["no onions"],
      "note": null,
      "count": 3,
      "unit_price_cents": 650,
      "total_cents": 1950
    }
  ],
  "per_person": [
    {
      "user_id": "…",
      "display_name": "anna",
      "items": [ /* order items */ ],
      "total_cents": 1300
    }
  ],
  "item_total_cents": 4550,
  "delivery_fee_cents": 250,
  "grand_total_cents": 4800,
  "min_order_value_cents": 2000,
  "below_minimum": false,
  "plain_text": "3x Döner Kebab (no onions)\n2x Lahmacun\n…"
}
```

Aggregation key: `menu_item_id` + the exact set of selected modification names +
the normalized free-text note (trimmed, case-insensitive). Items differing in
any of these are separate lines.

### Order event stream

`GET /orders/{id}/events` returns `text/event-stream`. One stream per order,
open to anonymous readers — but what a subscriber receives depends on whether
they are authenticated, mirroring the split in `GET /orders/{id}` (F7.4).

| Event             | Sent to        | Payload                                          |
| ----------------- | -------------- | ------------------------------------------------ |
| `item.created`    | authenticated  | The new order item.                               |
| `item.updated`    | authenticated  | The changed order item.                           |
| `item.deleted`    | authenticated  | `{"id": "…"}`.                                    |
| `order.item_count`| anonymous      | `{"item_count": 9}`. Replaces the three item events for anonymous subscribers. |
| `order.updated`   | everyone       | The changed order header.                         |
| `order.deleted`   | everyone       | `{"id": "…"}`. The client navigates away.         |
| `order.expired`   | everyone       | `{"id": "…"}`. The client switches to read-only.  |
| `ping`            | everyone       | Empty. Sent every 30 s to keep proxies honest.    |

The authentication state is captured when the stream is opened. A subscriber who
logs in afterwards reconnects and gets the authenticated event set; the frontend
does this automatically on login.

The order's hub publishes one logical change, and the handler decides per
subscriber which event that becomes. An anonymous subscriber must never receive
an item payload, so the count is computed and sent instead — a change that does
not alter the count still sends `order.item_count` with the unchanged value, so
that no inference can be drawn from the absence of an event.

The client reconnects automatically using the browser's built-in `EventSource`
retry and re-fetches the full order on reconnect rather than replaying missed
events. There is no `Last-Event-Id` support.

### Reference data

| Method | Path             | Access | Description                                  |
| ------ | ---------------- | ------ | -------------------------------------------- |
| `GET`  | `/tags`          | public | All free tags: id, code, and `name` for user-created ones. |
| `POST` | `/tags`          | user   | Create a free tag.                            |
| `GET`  | `/allergens`     | public | Seeded allergen list: id, code, `reference`, sort order. Read-only. |
| `GET`  | `/additives`     | public | Seeded additive list: id, code, `reference`, sort order. Read-only. |
| `GET`  | `/currencies`    | public | Seeded currency list: code, symbol, minor unit. |
| `GET`  | `/contact-types` | public | Seeded contact type list: id, code, `render_as`, sort order. |

**None of these endpoints returns a display name.** Seeded reference data is
identified by a stable `code` and named by the frontend's i18n catalog — see
[07_i18n.md](07_i18n.md#reference-data-names). A consumer without that catalog
gets the code, which is a deliberately self-describing English word, plus the
`reference` number where one exists.

The exception is `/tags`: a tag a user invented at runtime has no catalog entry,
so it carries the `name` they typed. Seeded tags have a catalog entry and their
`name` is ignored by the frontend.

### Images

| Method | Path            | Access | Description                                                     |
| ------ | --------------- | ------ | --------------------------------------------------------------- |
| `POST` | `/images`       | user   | `multipart/form-data` upload. Returns the created image record. |
| `GET`  | `/images/{id}`  | public | The stored image bytes.                                          |
| `GET`  | `/images/{id}/thumbnail` | public | The stored thumbnail bytes.                            |

Images are referenced by id from `restaurant.logo_image_id` and
`menu_item.image_id`. There is no `DELETE`; unreferenced images are removed by
the `cleanup` verb.

### Content pages

| Method | Path             | Access | Description                                          |
| ------ | ---------------- | ------ | ---------------------------------------------------- |
| `GET`  | `/pages/{key}`   | public | HTML snippet. `key` is `imprint` or `legal_notes`.   |
| `PUT`  | `/pages/{key}`   | admin  | Replace the snippet. The body is sanitized first.    |

Both directions carry `text/html; charset=utf-8` rather than JSON: what is
stored is a fragment of a document, and wrapping it in a JSON string would only
mean unwrapping it again. A key that is neither `imprint` nor `legal_notes` is
404 with error 4000.

`PUT` takes the snippet as the request body, sanitizes it with the allow-list in
`internal/htmlsafe` — the same policy the installer applies — and answers with
what was actually stored, so an administrator can see what survived. A body
larger than 256 KiB is refused with 1002, and one that is empty once sanitized
with 1001: both would leave a blank page, and the second is worth saying out
loud rather than storing silently.

### System

| Method | Path        | Access | Description                                                     |
| ------ | ----------- | ------ | --------------------------------------------------------------- |
| `GET`  | `/health`   | public | `{"status":"ok"}` with 200 when the database answers, otherwise `{"status":"error"}` with 503. |
| `GET`  | `/metrics`  | public | Counters, see below.                                             |
| `GET`  | `/version`  | public | Application version and applied schema version.                  |
| `POST` | `/shutdown` | admin  | Begin a graceful shutdown. Responds 202 before shutting down.    |
| `POST` | `/cleanup`  | admin  | Run the retention pass now: the same work the `cleanup` verb does from cron. Answers with what was removed. |

`/version` returns:

```json
{
  "version": "0.2.0",
  "commit": "7c81636",
  "build_date": "2026-09-06T09:12:44Z",
  "swagger": true,
  "max_image_size": 5242880
}
```

`swagger` says whether `/tools/swagger` is served. It is there for the frontend,
whose main menu hides the API documentation entry when it is not
([06_ui_ux.md](06_ui_ux.md)), and nothing else can tell it: `--no-swagger` omits
the route rather than answering 404, and an omitted non-`/api` path falls
through to the SPA fallback, which serves the application shell. A probe would
therefore report a Swagger UI that is not there.

`max_image_size` is `--max-image-size` in bytes. The upload control uses it to
refuse an oversized photograph before spending a minute sending it, and the
limit is the operator's to choose, so it cannot be a constant in the frontend.
The server enforces it regardless, with error 1011.

`/metrics` returns:

```json
{
  "restaurants": 7,
  "menu_items": 214,
  "orders_active": 2,
  "orders_expired": 31,
  "users": 24,
  "db_connections_open": 3,
  "db_connections_idle": 2,
  "db_connections_max": 10
}
```

The format is plain JSON rather than the Prometheus text exposition format. If
Prometheus scraping is ever needed, it belongs behind a separate path so this
endpoint stays stable.

## Non-API routes

| Path             | Description                                                              |
| ---------------- | ------------------------------------------------------------------------ |
| `/`              | The single-page frontend. Any unknown non-`/api` path also serves it, so client-side routing works on reload. |
| `/static/*`      | Frontend assets — CSS, JavaScript, fonts, images.                         |
| `/tools/swagger` | Swagger UI. Omitted entirely when `--no-swagger` is set.                  |
