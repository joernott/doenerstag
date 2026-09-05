# 01 Overview

doenerstag is a web application for coordinating food orders for a group of 1 to
n people. It keeps a persistent database of restaurants with their logo, contact
information and menu. Food orders can be created for a specific pickup or
delivery date and time and are visible to everyone who can reach the
application. Users add themselves to an order, pick one or more menu items and
attach predefined ("clickable") and free-text modifications to them. A summary
page shows a compact, aggregated list of everything that was ordered.

This application does not collect payments and does not place orders with
restaurants. It only helps organizing them. The actual order is placed by a
human, by phone or in person, using the summary page as their script.

## Deployment context

doenerstag is an intranet tool. It runs inside a company or club network where
every user who can reach the application is considered trusted. It is not
designed to be exposed to the public internet.

Consequences of that assumption run through the whole specification:

- Restaurants and menus are readable without logging in, as are the list of
  orders and each order's header and item count. The order items themselves —
  who ordered what — need a login, and an order's summary page is limited to
  the people taking part in it.
- Registration is open — anyone on the network can create an account.
- Any logged-in user may add restaurants, menu categories and menu items
  ("crowdsourcing" the data instead of requiring an administrator to enter it).
- Destructive operations are restricted to the administrator or the owner of the
  data (see [05_auth_and_permissions.md](05_auth_and_permissions.md)).
- The application must run without internet access at runtime.

## Expected scale

The specification is deliberately sized for small groups. Design decisions such
as "no pagination" and "no search" follow from these numbers.

| Dimension                        | Expected value |
| -------------------------------- | -------------- |
| Restaurants in the database      | 3 – 10         |
| Concurrent active orders per day | ~3             |
| Participants per order           | 1 – 30         |
| Registered users                 | tens           |

## Out of scope

The following are explicitly *not* part of the application:

- Payment collection, splitting or settlement.
- Placing orders with the restaurant electronically.
- Deadline reminders or any kind of push notification / e-mail.
- Multi-tenancy. One installation serves one group.
- Multiple time zones. Restaurant and participants are assumed to share one.
- Public internet exposure.
- Search and filtering of restaurants or orders.
- Sharing mechanisms (short links, QR codes). The application is promoted
  through the organization's own channels.

## Glossary

| Term              | Meaning                                                                                         |
| ----------------- | ----------------------------------------------------------------------------------------------- |
| **Order**         | One group food order at one restaurant for one pickup/delivery time.                              |
| **Order item**    | One line in an order: one user, one menu item, a quantity and its modifications.                  |
| **Deadline**      | The point in time after which no order items may be added or changed.                             |
| **Active order**  | An order whose deadline lies in the future.                                                       |
| **Expired order** | An order whose deadline has passed. Still readable, no longer editable.                           |
| **Modification**  | A change to a menu item — either a predefined, selectable option or free text.                    |
| **Tag**           | A free descriptive label on a menu item, e.g. `vegan`, `spicy`.                                   |
| **Allergen**      | An entry from the seeded, regulation-derived allergen list (EU 1169/2011 Annex II).               |
| **Additive**      | An entry from the seeded, regulation-derived food additive list (German ZZulV / EU 1333/2008).    |
| **Participant**   | Of an order: its creator, anyone holding at least one item in it, or the administrator.            |
| **Administrator** | The built-in `root` user. May delete restaurants, menu data and any order.                        |
| **Deleted user**  | Placeholder user `00000000-0000-7000-8000-000000000000`, owner of orphaned historical order items.|

## Document map

| Document                                                       | Contents                                                        |
| -------------------------------------------------------------- | --------------------------------------------------------------- |
| [01_overview.md](01_overview.md)                                 | This document. Scope, context, glossary.                        |
| [02_features.md](02_features.md)                                 | Functional requirements as user stories.                        |
| [03_data_model.md](03_data_model.md)                             | Entities, fields, constraints, seed data.                       |
| [04_api.md](04_api.md)                                           | REST API conventions and endpoints.                             |
| [05_auth_and_permissions.md](05_auth_and_permissions.md)         | Registration, login, sessions, tokens, permission matrix.       |
| [06_ui_ux.md](06_ui_ux.md)                                       | Screens, layout, states, accessibility.                         |
| [07_i18n.md](07_i18n.md)                                         | Languages, locale precedence, formatting rules.                 |
| [08_technologies.md](08_technologies.md)                         | Stack, project layout, build, logging.                          |
| [09_configuration.md](09_configuration.md)                       | Complete CLI, environment and config file reference.            |
| [10_operations.md](10_operations.md)                             | Install, update, cleanup, backup, monitoring.                   |
| [11_nonfunctional.md](11_nonfunctional.md)                       | Security, performance, browser support, accessibility targets.  |
| [12_testing.md](12_testing.md)                                   | Test strategy and coverage expectations.                        |
| [13_legal_and_privacy.md](13_legal_and_privacy.md)               | GDPR obligations, imprint and legal notes pages.                |
| [adr/](adr/)                                                     | Architecture decision records.                                  |
| [zz_prompts.md](zz_prompts.md)                                   | Development prompt log. Not part of the specification.          |
