-- Seeded reference data, per docs/03_data_model.md.
--
-- None of these tables stores a display name. Each row has a stable code and
-- the frontend translates it through its i18n catalog, so adding a language is
-- a catalog file rather than a migration. See docs/07_i18n.md.
--
-- The English and German wording each code stands for is documented in
-- docs/03_data_model.md and lives in frontend/src/i18n/*.json.
--
-- Seed rows use fixed UUIDs so that application code and tests can reference
-- them. The timestamp portion is zero — they are not generated UUIDv7 values —
-- but the version and variant nibbles are correct, and the fourth group
-- identifies the table:
--
--   0001  contact_type      0003  allergen
--   0002  tag               0004  additive
--
-- Because the application must run without internet access, every value below
-- is a literal. Nothing is downloaded at install time.

CREATE TABLE currency (
    code        char(3)     PRIMARY KEY,
    symbol      text        NOT NULL,
    minor_unit  smallint    NOT NULL,
    sort_order  integer     NOT NULL DEFAULT 0,

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT currency_code_is_uppercase_alpha CHECK (code ~ '^[A-Z]{3}$'),
    CONSTRAINT currency_minor_unit_range CHECK (minor_unit BETWEEN 0 AND 4)
);

COMMENT ON TABLE currency IS 'ISO 4217 currencies. The only table with a natural key rather than a UUID.';
COMMENT ON COLUMN currency.minor_unit IS 'Decimal digits: 2 for EUR, 0 for JPY. Amounts are stored in minor units.';

CREATE TRIGGER currency_set_updated_at
    BEFORE UPDATE ON currency
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO currency (code, symbol, minor_unit, sort_order) VALUES
    ('EUR', '€',   2, 10),
    ('CHF', 'CHF', 2, 20),
    ('GBP', '£',   2, 30),
    ('USD', '$',   2, 40),
    ('PLN', 'zł',  2, 50),
    ('CZK', 'Kč',  2, 60),
    ('DKK', 'kr',  2, 70),
    ('SEK', 'kr',  2, 80),
    ('NOK', 'kr',  2, 90),
    ('HUF', 'Ft',  2, 100);


-- Tells the frontend how to render each contact: a tel: link, a mailto: link,
-- a map link or plain text.
CREATE TABLE contact_type (
    id          uuid        PRIMARY KEY,
    code        text        NOT NULL UNIQUE,
    render_as   text        NOT NULL,
    sort_order  integer     NOT NULL DEFAULT 0,

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT contact_type_render_as_known
        CHECK (render_as IN ('tel', 'mailto', 'url', 'address', 'text'))
);

CREATE TRIGGER contact_type_set_updated_at
    BEFORE UPDATE ON contact_type
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO contact_type (id, code, render_as, sort_order) VALUES
    ('00000000-0000-7000-8000-000100000001', 'phone',   'tel',     10),
    ('00000000-0000-7000-8000-000100000002', 'mobile',  'tel',     20),
    ('00000000-0000-7000-8000-000100000003', 'fax',     'tel',     30),
    ('00000000-0000-7000-8000-000100000004', 'email',   'mailto',  40),
    ('00000000-0000-7000-8000-000100000005', 'website', 'url',     50),
    ('00000000-0000-7000-8000-000100000006', 'address', 'address', 60),
    ('00000000-0000-7000-8000-000100000007', 'other',   'text',    99);


-- Free descriptive labels. Unlike the other tables here, users may add rows at
-- runtime, which is why tag keeps a name column: an invented tag has no catalog
-- entry and can only carry the text someone typed.
CREATE TABLE tag (
    id          uuid        PRIMARY KEY,
    code        text        NOT NULL UNIQUE,
    name        text        NOT NULL,
    sort_order  integer     NOT NULL DEFAULT 0,

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT tag_code_shape CHECK (code ~ '^[a-z0-9_-]{1,64}$'),
    CONSTRAINT tag_name_length CHECK (char_length(name) BETWEEN 1 AND 64)
);

COMMENT ON COLUMN tag.name IS
    'Label for user-created tags. Ignored by the frontend for seeded tags, which are translated from tag.<code>.';

CREATE TRIGGER tag_set_updated_at
    BEFORE UPDATE ON tag
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO tag (id, code, name, sort_order) VALUES
    ('00000000-0000-7000-8000-000200000001', 'vegan',        'vegan',        10),
    ('00000000-0000-7000-8000-000200000002', 'vegetarian',   'vegetarian',   20),
    ('00000000-0000-7000-8000-000200000003', 'spicy',        'spicy',        30),
    ('00000000-0000-7000-8000-000200000004', 'very_spicy',   'very spicy',   40),
    ('00000000-0000-7000-8000-000200000005', 'halal',        'halal',        50),
    ('00000000-0000-7000-8000-000200000006', 'kosher',       'kosher',       60),
    ('00000000-0000-7000-8000-000200000007', 'gluten_free',  'gluten free',  70),
    ('00000000-0000-7000-8000-000200000008', 'lactose_free', 'lactose free', 80),
    ('00000000-0000-7000-8000-000200000009', 'new',          'new',          90),
    ('00000000-0000-7000-8000-00020000000a', 'signature',    'signature',    100);


-- The 14 substances that must be declared in the EU.
--
-- Source: Regulation (EU) No 1169/2011, Annex II.
--   https://eur-lex.europa.eu/eli/reg/2011/1169/oj
--
-- The list is legally fixed. Users cannot add rows, and the catalog wording is
-- a legal declaration rather than UI copy: it must not be reworded for tone or
-- brevity.
CREATE TABLE allergen (
    id          uuid        PRIMARY KEY,
    code        text        NOT NULL UNIQUE,
    reference   text        NOT NULL,
    sort_order  integer     NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL
);

COMMENT ON COLUMN allergen.reference IS 'Annex II item number, 1 to 14.';

CREATE TRIGGER allergen_set_updated_at
    BEFORE UPDATE ON allergen
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO allergen (id, code, reference, sort_order) VALUES
    ('00000000-0000-7000-8000-000300000001', 'gluten',       '1',  1),
    ('00000000-0000-7000-8000-000300000002', 'crustaceans',  '2',  2),
    ('00000000-0000-7000-8000-000300000003', 'eggs',         '3',  3),
    ('00000000-0000-7000-8000-000300000004', 'fish',         '4',  4),
    ('00000000-0000-7000-8000-000300000005', 'peanuts',      '5',  5),
    ('00000000-0000-7000-8000-000300000006', 'soy',          '6',  6),
    ('00000000-0000-7000-8000-000300000007', 'milk',         '7',  7),
    ('00000000-0000-7000-8000-000300000008', 'nuts',         '8',  8),
    ('00000000-0000-7000-8000-000300000009', 'celery',       '9',  9),
    ('00000000-0000-7000-8000-00030000000a', 'mustard',      '10', 10),
    ('00000000-0000-7000-8000-00030000000b', 'sesame',       '11', 11),
    ('00000000-0000-7000-8000-00030000000c', 'sulphites',    '12', 12),
    ('00000000-0000-7000-8000-00030000000d', 'lupin',        '13', 13),
    ('00000000-0000-7000-8000-00030000000e', 'molluscs',     '14', 14);


-- The additive declarations customary on German menus.
--
-- Sources: Zusatzstoff-Zulassungsverordnung (ZZulV)
--   https://www.gesetze-im-internet.de/zzulv_1998/
-- and Regulation (EC) No 1333/2008 on food additives
--   https://eur-lex.europa.eu/eli/reg/2008/1333/oj
--
-- Unlike the allergen list, the numbering is a restaurant convention rather
-- than a legally fixed scheme. reference is therefore nullable and is a display
-- hint, not an identifier.
CREATE TABLE additive (
    id          uuid        PRIMARY KEY,
    code        text        NOT NULL UNIQUE,
    reference   text,
    sort_order  integer     NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL
);

COMMENT ON COLUMN additive.reference IS
    'Conventional German menu number. A display hint, not an identifier: the numbering varies between restaurants.';

CREATE TRIGGER additive_set_updated_at
    BEFORE UPDATE ON additive
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO additive (id, code, reference, sort_order) VALUES
    ('00000000-0000-7000-8000-000400000001', 'colouring',     '1',  1),
    ('00000000-0000-7000-8000-000400000002', 'preservative',  '2',  2),
    ('00000000-0000-7000-8000-000400000003', 'antioxidant',   '3',  3),
    ('00000000-0000-7000-8000-000400000004', 'flavour_enh',   '4',  4),
    ('00000000-0000-7000-8000-000400000005', 'sulphured',     '5',  5),
    ('00000000-0000-7000-8000-000400000006', 'blackened',     '6',  6),
    ('00000000-0000-7000-8000-000400000007', 'waxed',         '7',  7),
    ('00000000-0000-7000-8000-000400000008', 'phosphate',     '8',  8),
    ('00000000-0000-7000-8000-000400000009', 'sweetener',     '9',  9),
    ('00000000-0000-7000-8000-00040000000a', 'phenylalanine', '10', 10),
    ('00000000-0000-7000-8000-00040000000b', 'caffeine',      '11', 11),
    ('00000000-0000-7000-8000-00040000000c', 'quinine',       '12', 12),
    ('00000000-0000-7000-8000-00040000000d', 'taurine',       '13', 13),
    ('00000000-0000-7000-8000-00040000000e', 'gmo',           '14', 14);
