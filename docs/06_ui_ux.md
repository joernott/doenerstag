# 06 User interface

## Principles

- **Desktop browsers first.** The primary environment is Firefox and Chrome on
  Windows and macOS. The layout is responsive and fully usable on tablets and
  phones, but the desktop layout is the one that is designed first and the one
  that constrains the others. This is explicitly *not* a mobile-first design.
- **Dark mode is the default.** A light mode is available and the choice is
  remembered in a cookie.
- **No loading states.** The application runs on a fast LAN. Data is fetched
  eagerly, images lazily via `loading="lazy"`, and the browser's own progress
  indication is considered sufficient. A spinner appears only if a request
  exceeds 2 seconds, which should not normally happen.
- **Every destructive action is confirmed.** A modal names the object being
  destroyed and, where relevant, the collateral effect ("3 other people have
  items in this order").
- **Nothing that cannot be done is shown as available.** Controls the current
  visitor is not permitted to use are hidden, not merely disabled — except where
  hiding them would make the page confusing, in which case they are disabled
  with an explanatory tooltip.

## Global chrome

### Title bar

Fixed to the top of the viewport on every page. Left to right:

| Element                 | Behaviour                                                                   |
| ----------------------- | --------------------------------------------------------------------------- |
| Application logo        | Always links back to the order overview.                                     |
| Application name        | "doenerstag". Hidden below the small breakpoint.                             |
| *(spacer)*              |                                                                              |
| Language selector       | Dropdown, English and German. Writes the `doener_lang` cookie.               |
| Dark/light toggle       | Icon button. Writes the `doener_theme` cookie.                               |
| Account control         | **Logged out:** a "Login / Register" button. **Logged in:** the display name, which links to the user page, followed by a logout icon button. |
| Menu button             | Opens the main dropdown menu.                                                |

### Main dropdown menu

| Entry            | Visible to      | Target                                    |
| ---------------- | --------------- | ----------------------------------------- |
| Orders           | everyone        | Order overview.                            |
| Restaurants      | everyone        | Restaurant overview.                       |
| My account       | logged-in users | User page.                                 |
| Users            | administrator   | User administration.                       |
| Version          | everyone        | Version page.                              |
| Imprint          | everyone        | Imprint page.                              |
| Legal notes      | everyone        | Legal notes page.                          |
| API documentation| everyone        | `/tools/swagger`. Hidden when `--no-swagger`. |

### Breakpoints

| Name | Width      | Layout                                                              |
| ---- | ---------- | ------------------------------------------------------------------- |
| `sm` | < 640 px   | One tile per row. Order page stacks: order data above, menu below.   |
| `md` | ≥ 768 px   | Two tiles per row. Order page still stacked.                         |
| `lg` | ≥ 1024 px  | Three tiles per row. Order page splits into two columns.             |
| `xl` | ≥ 1280 px  | Four tiles per row. Two columns with wider gutters.                  |

---

## Order overview (start page)

The default page. A grid of tiles.

- The **first tile** always carries a plus sign and creates a new order. For
  anonymous visitors it is shown but leads to the login page.
- When there are no orders at all, the plus tile is the only thing on the page.
  That is the empty state; there is no separate illustration or message.
- **Active orders** come first, sorted by fulfilment time ascending.
- **Expired orders** follow, sorted by fulfilment time descending, rendered
  faded (reduced opacity plus a muted border) but fully clickable.

Each tile shows:

| Element                | Notes                                                                 |
| ---------------------- | --------------------------------------------------------------------- |
| Restaurant logo        | Thumbnail. A neutral placeholder when the restaurant has no logo.      |
| Computed title         | `<restaurant name> — <fulfilment date> <fulfilment time>`, local time. |
| Fulfilment type        | Icon plus label: pickup or delivery.                                   |
| Deadline               | Local date and time, with a relative hint ("in 2 h") for active orders.|
| Participant count      | "5 people, 9 items".                                                   |
| Order total            | Formatted with the order's currency.                                   |
| Summary button         | Opens the summary page directly, skipping the order page.              |

## Order page

Two columns at `lg` and above; stacked below.

### Left column — the order

Read-only for everyone except the order creator and, for deletion, the
administrator.

- Restaurant name, logo, contacts (as clickable `tel:` / `mailto:` / map links),
  and opening hours.
- Fulfilment type, fulfilment time, deadline. All shown in local time.
- Money collector and pickup person, if set.
- Minimum order value and delivery fee, if set.
- Editing controls for the creator. The restaurant selector is disabled once the
  order has at least one item, with a tooltip explaining why.
- A **Summary** button.
- A **Delete order** button for the creator and the administrator.
- The list of order items, grouped by person, each showing quantity, item name,
  the selected modifications, the free-text note and the line total. A visitor's
  own items carry edit and delete buttons while the order is active.
- A running total, with the delivery fee and a warning when the total is below
  the minimum order value.
- When the deadline has passed, a prominent banner states the order is closed
  and all editing controls disappear.

### Right column — the menu

- Menu items grouped by category, categories in `sort_order`. When the
  restaurant has no categories, one flat list ordered by item ID then name.
- Each category header can be collapsed. The collapsed state is per-session and
  not persisted.
- A filter bar above the menu with three independent controls:
  1. **Tags** — multi-select. Shows items carrying any selected tag.
  2. **Allergens** — multi-select. *Excludes* items carrying any selected
     allergen.
  3. **Additives** — multi-select. *Excludes* items carrying any selected
     additive.
  The allergen and additive filters are exclusion filters and are labelled as
  such ("without…"), because that is how people actually use them.
- Each menu item row shows its thumbnail, item ID, name, description, price with
  currency symbol, tag chips and allergen/additive markers.
- Items marked unavailable are shown struck through with a "sold out" badge and
  cannot be added.
- Clicking an item opens the **add item** dialog: quantity stepper, checkbox
  list of the item's predefined modifications with their price deltas, a
  free-text field, and a live line total.
- An **Add menu item** button sits at the end of every category and at the
  bottom of the menu, so a missing item can be added without leaving the order.
- A "mark unavailable" control on each item for logged-in users.

### Live updates

The page subscribes to `GET /api/v1/orders/{id}/events`. Items added, changed or
removed by other people appear without a reload. When the `order.expired` event
arrives, the page switches itself to the read-only state. When `order.deleted`
arrives, the visitor is shown a message and returned to the overview.

## Summary page

Reached from the Summary button on the order page and from the summary button on
the order tile. Readable by anyone, including anonymous visitors, for active and
expired orders alike.

Four sections:

1. **Header** — computed order title, restaurant name and phone number as a
   prominent `tel:` link, fulfilment type and time, deadline and its state.
2. **What to order** — the aggregated list. One row per distinct combination of
   menu item, selected modifications and free-text note, with a count, the unit
   price and the row total. This is the section a person reads out on the phone,
   so it is the visually dominant one and uses a larger type size.
3. **Who owes what** — one block per participant with their items and their
   personal total. Deleted users appear as "deleted user".
4. **Totals** — item total, delivery fee, grand total, and a warning box when
   the total is below the restaurant's minimum order value.

Two actions:

- **Copy as text** — puts the plain-text rendering from the API on the
  clipboard.
- **Print** — the browser's own print dialog. A print stylesheet drops the title
  bar, navigation and buttons, forces light colours and keeps the aggregated
  list and totals on one page where possible.

## Restaurant overview

A grid of tiles, same shape as the order overview. The first tile carries a plus
sign and creates a restaurant. When no restaurants exist, the plus tile is the
only thing on the page.

Each tile shows the logo, name, the first contact entry, opening-hours status
("open now" / "closed") and the count of menu items.

There is no search and no sorting beyond alphabetical by name. With 3–10
restaurants expected, neither earns its place.

## Restaurant page

One page holding three stacked sections. Everything is editable by any
logged-in user; deletion is administrator-only.

1. **Restaurant data** — name, logo upload, currency selector, optional minimum
   order value and delivery fee, free-text notes. A "Delete restaurant" button
   for the administrator, disabled with an explanation when orders still
   reference it.
2. **Contacts** — a repeatable row of *type / value / label*, with the type
   coming from the seeded contact-type list. At least one row is required; the
   delete button on the last remaining row is disabled.
3. **Opening hours** — a row per entry: weekday, start time, end time. Multiple
   entries per weekday are allowed and rendered grouped by day. An entry whose
   end time precedes its start time is shown with a "crosses midnight" hint
   rather than an error.
4. **Menu** — categories with their items. Categories can be added, renamed and
   reordered by drag handle. Items can be added and edited inline, including
   their tags, allergens, additives and predefined modifications.

## User page

- **Anonymous visitor**: a combined login / register panel, two tabs.
- **Logged-in user**: forms for display name, user name, e-mail and password
  change; a list of API tokens with create and revoke controls; a
  "Delete my account" button.
- Deleting the account first calls `GET /users/{id}/deletion-impact` and shows a
  confirmation modal stating exactly what will happen: how many items in active
  orders will be deleted, how many items in expired orders will be reassigned to
  "deleted user", and how many orders they created will be reassigned. The
  visitor confirms by typing their user name.

## Administration pages

Visible only to `root`.

- **Users** — a table of all users with their creation date and last login, and
  a delete button per user following the same impact-confirmation flow.
- **Content pages** — for imprint and legal notes: the current rendered HTML,
  and an upload control to replace it with a new HTML snippet.

## Version, imprint and legal notes pages

- **Version** — application version, applied schema version and the timestamp
  it was applied, from `GET /api/v1/version`.
- **Imprint** and **Legal notes** — the stored HTML snippet, rendered inside the
  standard page frame. When the administrator is logged in, a "Replace content"
  button appears on each.

## Accessibility

Target: **WCAG 2.1 level AA**.

- Every function is reachable and operable by keyboard alone. No control is
  mouse-only, including the drag-handle reordering of categories, which has
  keyboard move-up / move-down alternatives.
- Visible focus indicators everywhere, meeting the 3:1 contrast requirement
  against both the focused component and the background.
- Text contrast at least 4.5:1, and 3:1 for large text and UI component
  boundaries, in **both** the dark and the light theme.
- Semantic HTML: real `<button>`, `<a>`, `<label>`, `<table>` elements. ARIA is
  used only where no native element fits — the tile grid, the modals and the
  live region announcing incoming order changes.
- Modals trap focus, close on `Escape` and return focus to the element that
  opened them.
- All images have `alt` text; decorative images use `alt=""`.
- Faded expired orders remain above the contrast minimum. Their state is also
  conveyed by a text label, never by colour or opacity alone.
- Allergen and tag markers are never colour-only; each carries text or an
  accessible name.
- The page has a skip-to-content link as its first focusable element.
- Form errors are associated with their field via `aria-describedby` and
  announced.

## Cookies

The frontend sets three cookies of its own, all non-`HttpOnly`, `SameSite=Lax`,
one year lifetime, and none of them used for tracking:

| Cookie         | Purpose                                     |
| -------------- | ------------------------------------------- |
| `doener_lang`  | Chosen interface language.                   |
| `doener_theme` | `dark` or `light`.                           |
| `doener_name`  | Last used login name, to prefill the form.   |

The session and CSRF cookies are described in
[05_auth_and_permissions.md](05_auth_and_permissions.md).
