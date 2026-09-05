# 02 Features

This document describes *what* the application does. The data behind it is
specified in [03_data_model.md](03_data_model.md), the screens in
[06_ui_ux.md](06_ui_ux.md), and who may do what in
[05_auth_and_permissions.md](05_auth_and_permissions.md).

## F1 — Browsing without an account

- **F1.1** Any visitor, logged in or not, can see the list of orders.
- **F1.2** Any visitor can open an order and see its items, who ordered them,
  their modifications and the running total.
- **F1.3** Any visitor can see the summary page of an order.
- **F1.4** Any visitor can see the restaurant list, restaurant details, opening
  hours and menus.
- **F1.5** Any visitor can see the version, imprint and legal notes pages.
- **F1.6** A visitor who is not logged in cannot change anything. Every control
  that would change data is hidden or disabled, and the API rejects the request.

## F2 — Accounts

- **F2.1** Anyone can register an account by choosing a user name and a
  password. An e-mail address and a display name are optional.
- **F2.2** User names are unique and case-insensitive for the uniqueness check.
- **F2.3** A user may change their user name, display name, password and e-mail
  address.
- **F2.4** A user may delete their own account. See F2.6.
- **F2.5** A user can be logged in from one browser at a time. Logging in
  elsewhere invalidates the previous session.
- **F2.6** Deleting an account happens immediately and irreversibly:
  - Order items belonging to the user in **expired** orders are reassigned to
    the deleted-user placeholder and shown as *"deleted user"*.
  - Order items belonging to the user in **active** orders are deleted.
  - Orders **created** by the user are reassigned to the deleted-user
    placeholder; the administrator remains able to delete them.
  - Before deleting, the user is shown how many active orders they still
    participate in and is given the chance to cancel.
- **F2.7** A user may create, list and revoke API tokens for third-party access.
- **F2.8** There is exactly one administrator account, `root`, created during
  installation. It cannot be deleted or renamed.

## F3 — Restaurants

- **F3.1** Any logged-in user can create a restaurant with at least a name, a
  currency and one contact entry.
- **F3.2** Any logged-in user can edit restaurant data, including adding and
  removing contact entries and opening hours.
- **F3.3** Restaurants may have a logo image.
- **F3.4** Restaurants may carry an optional minimum order value and an optional
  delivery fee. Both are copied into an order when the restaurant is chosen.
- **F3.5** Only the administrator can delete a restaurant, and only if no order
  references it.
- **F3.6** Opening hours are a list of weekday/start/end entries. Multiple
  entries per weekday are allowed (e.g. a lunch break). An entry whose end time
  is earlier than its start time is understood to cross midnight.

## F4 — Menus

- **F4.1** A menu belongs to exactly one restaurant.
- **F4.2** Any logged-in user can add menu categories and menu items, including
  from inside the order page while ordering.
- **F4.3** Menu items carry a name, price, optional restaurant-specific item ID,
  description, image, tags, allergens, additives and predefined modifications.
- **F4.4** Menu items can be marked temporarily unavailable ("sold out") by any
  logged-in user. Unavailable items are shown but cannot be ordered.
- **F4.5** Only the administrator can delete a menu item or category. Deletion is
  a soft delete; the row survives until no order item references it and the
  retention window has passed.
- **F4.6** If a restaurant has no categories, menu items are ordered by their
  restaurant-specific item ID and then by name.
- **F4.7** Menu items can be filtered in the UI by free tags and, independently,
  by allergens and additives.

## F5 — Orders

- **F5.1** Any logged-in user can create an order by choosing a restaurant, a
  fulfilment type (pickup or delivery), a fulfilment date/time and a deadline.
- **F5.2** An order can be created with no items and stays valid while empty.
- **F5.3** The deadline must lie strictly before the fulfilment time. The
  frontend validates this before submitting; the backend enforces it as well,
  because the API is also used by third parties.
- **F5.4** The frontend warns when the fulfilment time falls outside the
  restaurant's opening hours. This is a warning, not a hard error — restaurants
  do accept pre-orders.
- **F5.5** An order is **active** while `now < deadline` and **expired**
  afterwards. There is no manual state transition.
- **F5.6** An order has no stored title. Its display title is computed as
  *"&lt;restaurant name&gt; — &lt;fulfilment date&gt; &lt;fulfilment time&gt;"*.
- **F5.7** The order creator can edit the order's fields while it is active.
- **F5.8** Once the order has at least one item, the restaurant can no longer be
  changed.
- **F5.9** The order creator and the administrator can delete an order. For an
  active order this requires confirmation and warns how many participants will
  lose their items.
- **F5.10** An order carries two optional free-text fields: who collects the
  money and who does the pickup.
- **F5.11** An order stores a copy of the restaurant's currency, minimum order
  value and delivery fee as they were when the restaurant was selected.

## F6 — Order items

- **F6.1** Any logged-in user can add items to an **active** order.
- **F6.2** An order item references one menu item of the order's restaurant, a
  quantity of at least 1, an optional free-text modification and any number of
  the menu item's predefined modifications.
- **F6.3** A user may add several separate items for the same menu item, for
  example one with and one without onions.
- **F6.4** An order item stores a snapshot of the menu item's name and unit
  price, and of the name and price delta of each selected modification. Later
  price corrections never change a historical order.
- **F6.5** A user may edit and delete only their own order items, and only while
  the order is active.
- **F6.6** After the deadline, all order items become read-only for everyone,
  including the administrator.

## F7 — Live updates

- **F7.1** While an order page is open, changes made by other participants
  appear without a manual reload.
- **F7.2** This uses one Server-Sent Events stream per order. See
  [adr/0003-sse-for-order-updates.md](adr/0003-sse-for-order-updates.md).
- **F7.3** When the deadline passes while the page is open, the page switches
  itself into the read-only state.

## F8 — Summary

- **F8.1** Every order has a summary page, reachable from a button on the order
  page and from a button on the order's tile in the overview.
- **F8.2** The summary aggregates identical items — same menu item, same set of
  predefined modifications, same free text — into one line with a count.
- **F8.3** The summary shows a per-person breakdown with per-person totals.
- **F8.4** The summary shows the grand total, the delivery fee, and a warning if
  the order value is below the restaurant's minimum order value.
- **F8.5** The summary offers a plain-text rendering that can be copied to the
  clipboard, suitable for reading out on the phone.
- **F8.6** The summary is printable through the browser's own print function
  using a dedicated print stylesheet.

## F9 — Content pages

- **F9.1** The application serves an imprint page and a legal notes page. Both
  are HTML snippets stored in the database.
- **F9.2** Their initial content is loaded from files whose paths the operator
  is asked for during installation.
- **F9.3** The administrator can replace either page's content from the UI by
  uploading a new HTML snippet.
- **F9.4** Uploaded HTML is sanitized before it is stored. See
  [11_nonfunctional.md](11_nonfunctional.md).

## F10 — Operations

- **F10.1** A `version` page in the UI and a `version` CLI verb report the
  application version and the applied schema version.
- **F10.2** A health endpoint reports whether the database connection is up.
- **F10.3** A metrics endpoint reports object counts and database connection
  usage.
- **F10.4** The administrator can shut the application down through an API
  endpoint.
- **F10.5** A `cleanup` CLI verb removes orders whose deadline is older than the
  configured retention period, together with menu data that was soft-deleted and
  is no longer referenced.
- **F10.6** All data-changing operations are logged at INFO level with the acting
  user and a correlation ID. This is the audit trail.
