# 03 Data model

## Conventions

- **Database**: PostgreSQL 18. No other version is supported.
- **Primary keys**: `uuid`, generated as UUIDv7 by the application (not by the
  database), so that keys sort by creation time.
- **Time**: every point in time is stored as `timestamptz`. The database session
  runs with `TimeZone = 'UTC'`, so all values are stored and returned as UTC.
  Conversion to local time happens in the browser. Wall-clock values that are not
  points in time (opening hours) are stored as `time` and interpreted in the
  local time of the installation.
- **Money**: every monetary value is a `bigint` holding the amount in the
  currency's minor unit (cents). Never a floating point type. The currency comes
  from the owning restaurant.
- **Table naming**: singular, `snake_case`. `order` and `user` are reserved
  words in SQL, so the tables are named `food_order` and `app_user`.
- **Soft delete**: tables that may be referenced by historical order data carry
  `deleted_at timestamptz NULL`. A non-null value hides the row from all normal
  queries. Rows are physically removed by the `cleanup` verb once nothing
  references them any more.
- **Migrations**: managed with `golang-migrate`. Every schema change is a
  numbered up/down migration pair under `migrations/`.

### Audit columns

Every table carries these four columns:

| Column       | Type          | Null | Notes                                              |
| ------------ | ------------- | ---- | -------------------------------------------------- |
| `created_at` | `timestamptz` | no   | Default `now()`.                                    |
| `created_by` | `uuid`        | yes  | FK to `app_user.id`. Null for seeded/system rows.    |
| `updated_at` | `timestamptz` | no   | Default `now()`, maintained by a trigger.            |
| `updated_by` | `uuid`        | yes  | FK to `app_user.id`. Null for seeded/system rows.    |

`created_by`/`updated_by` use `ON DELETE SET NULL` so that deleting a user never
blocks. The human-readable audit trail is the INFO-level log, not these columns
(see [10_operations.md](10_operations.md)).

Audit columns are omitted from the per-table listings below for brevity.

---

## Entity relationship overview

```
app_user ──< session
         ──< api_token
         ──< food_order (creator)
         ──< order_item (owner)

restaurant ──< restaurant_contact >── contact_type
           ──< opening_hours
           ──< menu_category ──< menu_item
           ──< menu_item ──< menu_item_modification
                         ──< menu_item_tag      >── tag
                         ──< menu_item_allergen >── allergen
                         ──< menu_item_additive >── additive
                         ──  image (0..1)
           ──  image (logo, 0..1)
           ──  currency

food_order ── restaurant
           ──< order_item ── menu_item
                          ──< order_item_modification ── menu_item_modification (0..1)

content_page
app_version
```

---

## Core entities

### `app_user`

| Column          | Type          | Null | Notes                                                         |
| --------------- | ------------- | ---- | ------------------------------------------------------------- |
| `id`            | `uuid`        | no   | PK, UUIDv7.                                                    |
| `name`          | `text`        | no   | Login name. Unique on `lower(name)`. 3–64 chars.                |
| `display_name`  | `text`        | yes  | Shown in the UI. Falls back to `name` when null.                |
| `password_hash` | `text`        | no   | Argon2id PHC string. See [05](05_auth_and_permissions.md).      |
| `email`         | `text`        | yes  | Optional. Not verified. Not used for sending mail.              |
| `is_admin`      | `boolean`     | no   | Default `false`. True only for `root`.                          |
| `last_login_at` | `timestamptz` | yes  |                                                                 |

Constraints and seeded rows:

- `UNIQUE (lower(name))` via a unique expression index.
- The **deleted-user placeholder** is a seeded row with the fixed id
  `00000000-0000-7000-8000-000000000000`, name `deleted`, an unusable password
  hash and `is_admin = false`. It can never be logged into or deleted.
- The **administrator** `root` is created during `doenerstag install` with a
  password chosen interactively. `is_admin = true`.

### `session`

Server-side companion to the JWT. Required because a stateless JWT cannot
express an idle timeout or single-session enforcement.

| Column             | Type          | Null | Notes                                              |
| ------------------ | ------------- | ---- | -------------------------------------------------- |
| `id`               | `uuid`        | no   | PK, UUIDv7. Carried in the JWT `jti` claim.         |
| `user_id`          | `uuid`        | no   | FK `app_user`, `ON DELETE CASCADE`.                 |
| `issued_at`        | `timestamptz` | no   |                                                     |
| `last_seen_at`     | `timestamptz` | no   | Updated on each authenticated request.              |
| `absolute_expires` | `timestamptz` | no   | `issued_at + absolute-timeout`.                     |
| `remote_addr`      | `text`        | yes  |                                                     |
| `user_agent`       | `text`        | yes  |                                                     |

- `UNIQUE (user_id)` — one active session per user (F2.5). A new login replaces
  the row.
- Expired rows are removed by the `cleanup` verb and lazily on access.

### `api_token`

| Column         | Type          | Null | Notes                                                     |
| -------------- | ------------- | ---- | --------------------------------------------------------- |
| `id`           | `uuid`        | no   | PK, UUIDv7.                                                |
| `user_id`      | `uuid`        | no   | FK `app_user`, `ON DELETE CASCADE`.                        |
| `name`         | `text`        | no   | User-chosen label, unique per user.                        |
| `token_hash`   | `text`        | no   | SHA-256 of the token. The token itself is never stored.    |
| `token_prefix` | `text`        | no   | First 8 chars, shown in the UI so tokens are identifiable. |
| `expires_at`   | `timestamptz` | yes  | Null means no expiry.                                      |
| `last_used_at` | `timestamptz` | yes  |                                                            |

---

## Restaurant entities

### `restaurant`

| Column                  | Type          | Null | Notes                                                     |
| ----------------------- | ------------- | ---- | --------------------------------------------------------- |
| `id`                    | `uuid`        | no   | PK, UUIDv7.                                                |
| `name`                  | `text`        | no   | Unique on `lower(name)` among non-deleted rows.            |
| `logo_image_id`         | `uuid`        | yes  | FK `image`, `ON DELETE SET NULL`.                          |
| `currency_code`         | `char(3)`     | no   | FK `currency.code`. Applies to the whole menu.             |
| `min_order_value_cents` | `bigint`      | yes  | Optional. Minimum order value for delivery.                |
| `delivery_fee_cents`    | `bigint`      | yes  | Optional.                                                  |
| `notes`                 | `text`        | yes  | Free text, e.g. "no card payment".                         |
| `deleted_at`            | `timestamptz` | yes  | Soft delete.                                               |

A restaurant needs at least one `restaurant_contact` row. This is enforced by
the application, not by a database constraint.

### `contact_type`

Seeded helper table so that the frontend can render each contact correctly
(`tel:` link, `mailto:` link, map link, plain text).

| Column       | Type      | Null | Notes                                             |
| ------------ | --------- | ---- | ------------------------------------------------- |
| `id`         | `uuid`    | no   | PK, UUIDv7.                                        |
| `code`       | `text`    | no   | Unique. Stable identifier used by the frontend.    |
| `render_as`  | `text`    | no   | One of `tel`, `mailto`, `url`, `address`, `text`.  |
| `sort_order` | `integer` | no   |                                                    |

Seeded rows:

| `code`    | `render_as` | `sort_order` | English label   | German label      |
| --------- | ----------- | ------------ | --------------- | ----------------- |
| `phone`   | `tel`       | 10           | Phone           | Telefon           |
| `mobile`  | `tel`       | 20           | Mobile          | Mobil             |
| `fax`     | `tel`       | 30           | Fax             | Fax               |
| `email`   | `mailto`    | 40           | E-mail          | E-Mail            |
| `website` | `url`       | 50           | Website         | Webseite          |
| `address` | `address`   | 60           | Address         | Adresse           |
| `other`   | `text`      | 99           | Other           | Sonstiges         |

Labels are not stored; the frontend translates the `code` through its i18n
catalog (see [07_i18n.md](07_i18n.md)). The table above documents the expected
translations.

### `restaurant_contact`

| Column            | Type      | Null | Notes                                            |
| ----------------- | --------- | ---- | ------------------------------------------------ |
| `id`              | `uuid`    | no   | PK, UUIDv7.                                       |
| `restaurant_id`   | `uuid`    | no   | FK `restaurant`, `ON DELETE CASCADE`.             |
| `contact_type_id` | `uuid`    | no   | FK `contact_type`, `ON DELETE RESTRICT`.          |
| `value`           | `text`    | no   | The phone number, address, URL, …                 |
| `label`           | `text`    | yes  | Optional qualifier, e.g. "kitchen", "branch 2".   |
| `sort_order`      | `integer` | no   | Default 0.                                        |

The pickup address is modelled as a contact of type `address`; there is no
separate address column on `restaurant`.

### `opening_hours`

| Column          | Type      | Null | Notes                                                     |
| --------------- | --------- | ---- | --------------------------------------------------------- |
| `id`            | `uuid`    | no   | PK, UUIDv7.                                                |
| `restaurant_id` | `uuid`    | no   | FK `restaurant`, `ON DELETE CASCADE`.                      |
| `day_of_week`   | `smallint`| no   | ISO-8601: 1 = Monday … 7 = Sunday. `CHECK (1..7)`.         |
| `start_time`    | `time`    | no   | Local wall-clock time.                                     |
| `end_time`      | `time`    | no   | Local wall-clock time.                                     |

- Several rows per weekday are allowed, e.g. `11:00–14:30` and `17:00–22:00`.
- `end_time < start_time` means the period crosses midnight, e.g.
  `22:00–02:00` on Friday runs into Saturday morning.
- `end_time = start_time` is rejected.

---

## Menu entities

### `menu_category`

| Column          | Type          | Null | Notes                                               |
| --------------- | ------------- | ---- | --------------------------------------------------- |
| `id`            | `uuid`        | no   | PK, UUIDv7.                                          |
| `restaurant_id` | `uuid`        | no   | FK `restaurant`, `ON DELETE CASCADE`.                |
| `name`          | `text`        | no   |                                                      |
| `sort_order`    | `integer`     | no   | Default 0. Ties are broken by `name`.                |
| `deleted_at`    | `timestamptz` | yes  | Soft delete.                                         |

- `UNIQUE (restaurant_id, lower(name)) WHERE deleted_at IS NULL`.
- The field is named `sort_order`, not `order`, to avoid the SQL keyword and the
  clash with the order entity.

### `menu_item`

| Column          | Type          | Null | Notes                                                       |
| --------------- | ------------- | ---- | ----------------------------------------------------------- |
| `id`            | `uuid`        | no   | PK, UUIDv7.                                                  |
| `restaurant_id` | `uuid`        | no   | FK `restaurant`, `ON DELETE CASCADE`.                        |
| `category_id`   | `uuid`        | yes  | FK `menu_category`, `ON DELETE SET NULL`.                    |
| `external_id`   | `text`        | yes  | The restaurant's own item number, e.g. `A12`. Alphanumeric.  |
| `name`          | `text`        | no   |                                                              |
| `description`   | `text`        | yes  |                                                              |
| `image_id`      | `uuid`        | yes  | FK `image`, `ON DELETE SET NULL`.                            |
| `price_cents`   | `bigint`      | no   | `CHECK (price_cents >= 0)`. Currency from the restaurant.    |
| `available`     | `boolean`     | no   | Default `true`. `false` = temporarily sold out.              |
| `deleted_at`    | `timestamptz` | yes  | Soft delete.                                                 |

- `UNIQUE (restaurant_id, lower(name)) WHERE deleted_at IS NULL`.
- Display order is always `external_id`, then `name` (F4.6) — within a category
  and in the flat list alike. A purely numeric `external_id` sorts numerically so
  that 2 precedes 10; a mixed ID sorts as text. `NULLS LAST`, so items without a
  restaurant ID come after the numbered ones.

  ```sql
  ORDER BY external_id IS NULL,
           (CASE WHEN external_id ~ '^[0-9]+$'
                 THEN external_id::bigint END) NULLS LAST,
           external_id,
           name
  ```

- There is deliberately **no** `sort_order` on `menu_item`. Ordering follows the
  restaurant's own item numbering, which is what the printed menu does, so a
  separate manual ordering column would have nothing to do. Categories keep
  their `sort_order`, because category order is a genuine editorial choice.
- There is no per-item currency. Currency lives on the restaurant.

### `menu_item_modification`

The predefined, clickable modifications offered for a menu item. Free-text
modifications are not stored here — they live in `order_item.note`.

| Column             | Type          | Null | Notes                                                  |
| ------------------ | ------------- | ---- | ------------------------------------------------------ |
| `id`               | `uuid`        | no   | PK, UUIDv7.                                             |
| `menu_item_id`     | `uuid`        | no   | FK `menu_item`, `ON DELETE CASCADE`.                    |
| `name`             | `text`        | no   | E.g. "no onions", "extra garlic sauce", "large".        |
| `price_delta_cents`| `bigint`      | no   | Default 0. May be negative.                             |
| `sort_order`       | `integer`     | no   | Default 0.                                              |
| `deleted_at`       | `timestamptz` | yes  | Soft delete.                                            |

- `UNIQUE (menu_item_id, lower(name)) WHERE deleted_at IS NULL`.
- Modifications are a flat, freely combinable list rendered as checkboxes. There
  are deliberately no mutually exclusive groups ("size: S/M/L" as radio buttons)
  in this version; see
  [adr/0007-flat-modification-list.md](adr/0007-flat-modification-list.md).

---

## Classification entities

Free tags and regulated classifications are kept in separate tables so that the
UI can filter by each independently (F4.7).

### `tag`

Free descriptive labels. Any logged-in user may create one.

| Column       | Type      | Null | Notes                                                      |
| ------------ | --------- | ---- | ---------------------------------------------------------- |
| `id`         | `uuid`    | no   | PK, UUIDv7.                                                 |
| `code`       | `text`    | no   | Unique, lowercase, `[a-z0-9_-]+`. i18n key for seeded tags — `tag.vegan`. |
| `name`       | `text`    | no   | The label for user-created tags, which have no catalog entry. Ignored by the frontend for seeded tags. |
| `sort_order` | `integer` | no   | Default 0.                                                  |

Seeded tags: `vegan`, `vegetarian`, `spicy`, `very_spicy`, `halal`, `kosher`,
`gluten_free`, `lactose_free`, `new`, `signature`.

### `allergen`

Seeded from **Regulation (EU) No 1169/2011, Annex II** — the 14 substances that
must be declared in the EU. The list is legally fixed; users cannot add rows.

| Column       | Type      | Null | Notes                                                   |
| ------------ | --------- | ---- | ------------------------------------------------------- |
| `id`         | `uuid`    | no   | PK, UUIDv7. Fixed values in the seed migration.           |
| `code`       | `text`    | no   | Unique, e.g. `gluten`. i18n key — `allergen.gluten`.      |
| `reference`  | `text`    | no   | Annex II item number, `1`–`14`.                           |
| `sort_order` | `integer` | no   | Equals the Annex II number.                               |

No name columns. The display text for each row lives in the frontend catalogs
under `allergen.<code>` — see [07_i18n.md](07_i18n.md#reference-data-names).

Seed data. The English and German columns below are **documentation of what each
code means**, and the content of the `en` and `de` catalog entries. They are not
database columns:

| # | `code`        | `allergen.<code>` in `en`        | in `de`                         |
| - | ------------- | -------------------------------- | ------------------------------- |
| 1 | `gluten`      | Cereals containing gluten        | Glutenhaltiges Getreide         |
| 2 | `crustaceans` | Crustaceans                      | Krebstiere                      |
| 3 | `eggs`        | Eggs                             | Eier                            |
| 4 | `fish`        | Fish                             | Fisch                           |
| 5 | `peanuts`     | Peanuts                          | Erdnüsse                        |
| 6 | `soy`         | Soybeans                         | Sojabohnen                      |
| 7 | `milk`        | Milk (including lactose)         | Milch (einschließlich Laktose)  |
| 8 | `nuts`        | Tree nuts                        | Schalenfrüchte                  |
| 9 | `celery`      | Celery                           | Sellerie                        |
|10 | `mustard`     | Mustard                          | Senf                            |
|11 | `sesame`      | Sesame seeds                     | Sesamsamen                      |
|12 | `sulphites`   | Sulphur dioxide and sulphites    | Schwefeldioxid und Sulfite      |
|13 | `lupin`       | Lupin                            | Lupinen                         |
|14 | `molluscs`    | Molluscs                         | Weichtiere                      |

Source, to be recorded in the seed migration and in a comment above the catalog
block: `https://eur-lex.europa.eu/eli/reg/2011/1169/oj` (Annex II).

Because the wording is regulatory, the catalog entries for allergens are not
ordinary UI strings and must not be reworded for tone or brevity. A change to
one is a change to a legal declaration.

### `additive`

Seeded with the additive declarations customary on German menus. Unlike the
allergen list, the *numbering* is a restaurant convention rather than a legally
fixed scheme; the underlying declaration obligations come from the
Zusatzstoff-Zulassungsverordnung (ZZulV) and Regulation (EC) No 1333/2008.
The `reference` column therefore holds the conventional number and should be
treated as a display hint, not as an identifier.

| Column       | Type      | Null | Notes                                       |
| ------------ | --------- | ---- | ------------------------------------------- |
| `id`         | `uuid`    | no   | PK, UUIDv7. Fixed values in the seed.        |
| `code`       | `text`    | no   | Unique, e.g. `colouring`. i18n key — `additive.colouring`. |
| `reference`  | `text`    | yes  | Conventional German menu number.             |
| `sort_order` | `integer` | no   |                                              |

No name columns, for the same reason as `allergen`. Display text lives under
`additive.<code>` in the catalogs.

Seed data. As above, the English and German columns are documentation and
catalog content, not database columns:

| #  | `code`          | `additive.<code>` in `en`        | in `de`                           |
| -- | --------------- | -------------------------------- | --------------------------------- |
| 1  | `colouring`     | With colouring                   | Mit Farbstoff                     |
| 2  | `preservative`  | With preservative                | Mit Konservierungsstoff           |
| 3  | `antioxidant`   | With antioxidant                 | Mit Antioxidationsmittel          |
| 4  | `flavour_enh`   | With flavour enhancer            | Mit Geschmacksverstärker          |
| 5  | `sulphured`     | Sulphured                        | Geschwefelt                       |
| 6  | `blackened`     | Blackened                        | Geschwärzt                        |
| 7  | `waxed`         | Waxed                            | Gewachst                          |
| 8  | `phosphate`     | With phosphate                   | Mit Phosphat                      |
| 9  | `sweetener`     | With sweetener                   | Mit Süßungsmittel                 |
| 10 | `phenylalanine` | Contains a source of phenylalanine | Enthält eine Phenylalaninquelle |
| 11 | `caffeine`      | Contains caffeine                | Koffeinhaltig                     |
| 12 | `quinine`       | Contains quinine                 | Chininhaltig                      |
| 13 | `taurine`       | With taurine                     | Mit Taurin                        |
| 14 | `gmo`           | Genetically modified             | Gentechnisch verändert            |

Sources, to be recorded in the seed migration:
`https://www.gesetze-im-internet.de/zzulv_1998/` (ZZulV) and
`https://eur-lex.europa.eu/eli/reg/2008/1333/oj` (EC 1333/2008).

Because the application must run without internet access, all seed data ships as
literal `INSERT` statements inside the migration files. Nothing is downloaded at
install time.

### Where seeded names live

None of the seeded reference tables — `currency`, `allergen`, `additive`,
`contact_type` — stores a display name. Each stores a stable `code`, and the
frontend translates it through its i18n catalog. Adding a language is a catalog
file, never a migration.

The dividing line is who created the row:

| Row origin                          | Name comes from                                   |
| ----------------------------------- | ------------------------------------------------- |
| Seeded by a migration               | The i18n catalog, keyed on `code`.                 |
| Created by a user at runtime        | A `name` column in the database.                   |

That is why `tag` keeps its `name` column: seeded tags like `vegan` are
translated from `tag.vegan`, but a tag a user invents at runtime has no catalog
entry and can only carry the text they typed. Restaurant, menu item and category
names are user content and are likewise never translated.

### Link tables

`menu_item_tag`, `menu_item_allergen` and `menu_item_additive` all follow the
same shape:

| Column         | Type   | Null | Notes                                            |
| -------------- | ------ | ---- | ------------------------------------------------ |
| `menu_item_id` | `uuid` | no   | FK `menu_item`, `ON DELETE CASCADE`.              |
| `<other>_id`   | `uuid` | no   | FK to `tag` / `allergen` / `additive`, RESTRICT.  |

Primary key is the column pair. These tables carry audit columns as well.

---

## Order entities

### `food_order`

| Column                  | Type          | Null | Notes                                                    |
| ----------------------- | ------------- | ---- | -------------------------------------------------------- |
| `id`                    | `uuid`        | no   | PK, UUIDv7.                                               |
| `creator_id`            | `uuid`        | no   | FK `app_user`, `ON DELETE SET DEFAULT` → deleted user.    |
| `restaurant_id`         | `uuid`        | no   | FK `restaurant`, `ON DELETE RESTRICT`.                    |
| `fulfilment`            | `text`        | no   | `pickup` or `delivery`. `CHECK`ed.                        |
| `fulfilment_at`         | `timestamptz` | no   | When the food is picked up or delivered.                  |
| `deadline_at`           | `timestamptz` | no   | Last moment items may be added or changed.                |
| `money_collector`       | `text`        | yes  | Free text: who collects the money.                        |
| `pickup_person`         | `text`        | yes  | Free text: who fetches the order.                         |
| `currency_code`         | `char(3)`     | no   | Copied from the restaurant at creation.                   |
| `min_order_value_cents` | `bigint`      | yes  | Copied from the restaurant at creation.                   |
| `delivery_fee_cents`    | `bigint`      | yes  | Copied from the restaurant at creation.                   |

Constraints:

- `CHECK (deadline_at < fulfilment_at)`.
- There is no stored title and no stored status. Both are derived:
  - Title = `"<restaurant.name> — <fulfilment_at in local time>"` (F5.6).
  - Status = `active` while `now() < deadline_at`, otherwise `expired` (F5.5).
- The copied currency / minimum value / delivery fee are snapshots. Changing the
  restaurant afterwards does not update existing orders.
- `restaurant_id` may only be changed while the order has zero items (F5.8).

Indexes: `(deadline_at)` for the overview and the `cleanup` verb,
`(restaurant_id)`.

### `order_item`

| Column            | Type      | Null | Notes                                                    |
| ----------------- | --------- | ---- | -------------------------------------------------------- |
| `id`              | `uuid`    | no   | PK, UUIDv7.                                               |
| `order_id`        | `uuid`    | no   | FK `food_order`, `ON DELETE CASCADE`.                     |
| `user_id`         | `uuid`    | no   | FK `app_user`, `ON DELETE SET DEFAULT` → deleted user.    |
| `menu_item_id`    | `uuid`    | no   | FK `menu_item`, `ON DELETE RESTRICT`.                     |
| `quantity`        | `integer` | no   | `CHECK (quantity >= 1)`.                                  |
| `item_name`       | `text`    | no   | **Snapshot** of `menu_item.name` at ordering time.        |
| `unit_price_cents`| `bigint`  | no   | **Snapshot** of `menu_item.price_cents`.                  |
| `note`            | `text`    | yes  | Free-text modification.                                   |

- The snapshot columns are the authoritative values for display and totals.
  `menu_item_id` exists only for grouping in the summary and for the "still on
  the menu?" check.
- Several rows for the same `(order_id, user_id, menu_item_id)` are allowed
  (F6.3). There is no unique constraint on that triple.

Index: `(order_id)`, `(user_id)`, `(menu_item_id)` — the last one is needed by
`cleanup` to find unreferenced soft-deleted menu items.

### `order_item_modification`

| Column              | Type     | Null | Notes                                                    |
| ------------------- | -------- | ---- | -------------------------------------------------------- |
| `id`                | `uuid`   | no   | PK, UUIDv7.                                               |
| `order_item_id`     | `uuid`   | no   | FK `order_item`, `ON DELETE CASCADE`.                     |
| `modification_id`   | `uuid`   | yes  | FK `menu_item_modification`, `ON DELETE SET NULL`.        |
| `name`              | `text`   | no   | **Snapshot** of the modification name.                    |
| `price_delta_cents` | `bigint` | no   | **Snapshot** of the price delta.                          |

Line total for an order item:

```
quantity * (unit_price_cents + SUM(order_item_modification.price_delta_cents))
```

---

## Supporting entities

### `image`

Images live in the database, not on the filesystem. Uploads are re-encoded and
downscaled by the backend before storage, so the stored bytes are always within
the display size the layout needs.

| Column              | Type      | Null | Notes                                                     |
| ------------------- | --------- | ---- | --------------------------------------------------------- |
| `id`                | `uuid`    | no   | PK, UUIDv7.                                                |
| `media_type`        | `text`    | no   | `image/jpeg`, `image/png` or `image/gif` only.             |
| `width`             | `integer` | no   | Of the stored (downscaled) image.                          |
| `height`            | `integer` | no   |                                                            |
| `byte_size`         | `integer` | no   | Length of `data`.                                          |
| `data`              | `bytea`   | no   | The downscaled image.                                      |
| `thumb_media_type`  | `text`    | no   |                                                            |
| `thumb_width`       | `integer` | no   |                                                            |
| `thumb_height`      | `integer` | no   |                                                            |
| `thumb_data`        | `bytea`   | no   | Thumbnail used on tiles and menu rows.                     |
| `sha256`            | `bytea`   | no   | Of the stored image. Used to deduplicate uploads.          |
| `original_filename` | `text`    | yes  | For reference only.                                        |

Rules:

- Accepted upload media types: **JPEG, PNG, GIF**. Anything else is rejected
  with a validation error. The type is determined by sniffing the content, not
  by the file extension or the declared `Content-Type`.
- The maximum accepted upload size is configurable (`--max-image-size`, see
  [09_configuration.md](09_configuration.md)). Larger uploads are rejected with
  HTTP 413.
- Downscaling targets: main image max 800×800 px, thumbnail max 200×200 px,
  aspect ratio preserved, never upscaled. Animated GIFs are reduced to their
  first frame.
- Images are served with a long `Cache-Control` and a strong `ETag` derived from
  `sha256`; the id never changes for given content.
- Orphaned images (referenced by nothing) are removed by the `cleanup` verb.

### `currency`

Seeded lookup table so the UI can render a symbol next to every price.

| Column       | Type       | Null | Notes                                            |
| ------------ | ---------- | ---- | ------------------------------------------------ |
| `code`       | `char(3)`  | no   | PK. ISO 4217 alphabetic code. i18n key.           |
| `symbol`     | `text`     | no   | E.g. `€`, `$`, `CHF`.                             |
| `minor_unit` | `smallint` | no   | Decimal digits. 2 for EUR, 0 for JPY.             |
| `sort_order` | `integer`  | no   | Puts the likely candidates at the top of the list.|

Seeded with `EUR`, `CHF`, `GBP`, `USD`, `PLN`, `CZK`, `DKK`, `SEK`, `NOK`,
`HUF`. `EUR` is the default offered when creating a restaurant.

Currency **names** are not stored — the frontend translates the ISO code through
`currency.EUR`, as described under
[Where seeded names live](#where-seeded-names-live). A currency the catalog does
not know falls back to displaying its code, which is universally understood.

The `symbol` and `minor_unit` columns stay in the database: they are properties
of the currency itself, identical in every language, and the backend needs
`minor_unit` to validate and format amounts.

`currency` is the only table exempt from the UUID primary key rule — the ISO
code is a better natural key and appears in every price payload.

### `content_page`

| Column | Type   | Null | Notes                                                    |
| ------ | ------ | ---- | -------------------------------------------------------- |
| `id`   | `uuid` | no   | PK, UUIDv7.                                               |
| `key`  | `text` | no   | Unique. `imprint` or `legal_notes`.                       |
| `html` | `text` | no   | Sanitized HTML snippet, no `<html>`/`<head>`/`<body>`.    |

Both rows are created during `doenerstag install` from files whose paths the
operator supplies (F9.2). If a path is left empty, the row is created with a
placeholder text. The administrator can replace the content later (F9.3).

### `app_version`

| Column           | Type          | Null | Notes                                             |
| ---------------- | ------------- | ---- | ------------------------------------------------- |
| `id`             | `uuid`        | no   | PK, UUIDv7.                                        |
| `major`          | `integer`     | no   | Semantic version.                                  |
| `minor`          | `integer`     | no   |                                                    |
| `patch`          | `integer`     | no   |                                                    |
| `schema_version` | `bigint`      | no   | golang-migrate version applied by this release.    |
| `applied_at`     | `timestamptz` | no   |                                                    |

One row is appended by every `install` and `update` run. The `version` verb and
the version page report the row with the newest `applied_at`. This table is the
application's own history; `golang-migrate` maintains its separate
`schema_migrations` table, which the application never writes to directly.

---

## Referential integrity and deletion

| Action                          | Effect                                                                                      |
| ------------------------------- | ------------------------------------------------------------------------------------------- |
| Delete user (self or admin)     | Items in expired orders → deleted-user placeholder. Items in active orders → deleted. Orders created by them → placeholder. Sessions and tokens → cascade. |
| Delete restaurant (admin)       | Rejected while any `food_order` references it. Otherwise soft delete.                        |
| Delete menu category (admin)    | Soft delete. Items keep their `category_id` until `cleanup` nulls it.                        |
| Delete menu item (admin)        | Soft delete. Hidden from menus immediately.                                                  |
| Delete modification (admin)     | Soft delete. Snapshots in existing order items are unaffected.                               |
| Delete order (creator or admin) | Hard delete, cascading to items and item modifications.                                      |
| `cleanup`                       | Deletes orders past retention, then physically removes soft-deleted menu data that nothing references, then orphaned images, then expired sessions. |
