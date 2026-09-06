-- Menu categories, items, their modifications and their classification, per
-- docs/03_data_model.md.

CREATE TABLE menu_category (
    id             uuid        PRIMARY KEY,
    restaurant_id  uuid        NOT NULL REFERENCES restaurant (id) ON DELETE CASCADE,
    name           text        NOT NULL,
    sort_order     integer     NOT NULL DEFAULT 0,
    deleted_at     timestamptz,

    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT menu_category_name_length
        CHECK (char_length(name) BETWEEN 1 AND 200)
);

-- Named sort_order, not "order": the SQL keyword aside, "order" already means
-- something else entirely in this schema.
COMMENT ON COLUMN menu_category.sort_order IS
    'Editorial ordering of categories. Menu items within a category are not ordered by hand.';

CREATE UNIQUE INDEX menu_category_name_key
    ON menu_category (restaurant_id, lower(name))
    WHERE deleted_at IS NULL;

CREATE TRIGGER menu_category_set_updated_at
    BEFORE UPDATE ON menu_category
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE menu_item (
    id             uuid        PRIMARY KEY,
    restaurant_id  uuid        NOT NULL REFERENCES restaurant (id) ON DELETE CASCADE,
    category_id    uuid        REFERENCES menu_category (id) ON DELETE SET NULL,
    external_id    text,
    name           text        NOT NULL,
    description    text,
    image_id       uuid        REFERENCES image (id) ON DELETE SET NULL,
    price_cents    bigint      NOT NULL,
    available      boolean     NOT NULL DEFAULT true,
    deleted_at     timestamptz,

    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT menu_item_name_length
        CHECK (char_length(name) BETWEEN 1 AND 200),
    CONSTRAINT menu_item_external_id_shape
        CHECK (external_id IS NULL OR external_id ~ '^[A-Za-z0-9._-]{1,32}$'),
    CONSTRAINT menu_item_price_not_negative
        CHECK (price_cents >= 0)
);

COMMENT ON COLUMN menu_item.external_id IS
    'The restaurant''s own item number. Menus are ordered by it, then by name.';
COMMENT ON COLUMN menu_item.available IS
    'False means temporarily sold out: shown, but not orderable.';
COMMENT ON COLUMN menu_item.price_cents IS
    'Minor units of the restaurant''s currency. There is deliberately no per-item currency.';

-- There is no sort_order on menu_item. Ordering follows the restaurant's own
-- item numbering, which is what the printed menu does, so a manual ordering
-- column would have nothing to do.

CREATE UNIQUE INDEX menu_item_name_key
    ON menu_item (restaurant_id, lower(name))
    WHERE deleted_at IS NULL;

CREATE INDEX menu_item_restaurant_idx ON menu_item (restaurant_id) WHERE deleted_at IS NULL;
CREATE INDEX menu_item_category_idx ON menu_item (category_id);

CREATE TRIGGER menu_item_set_updated_at
    BEFORE UPDATE ON menu_item
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Predefined, clickable modifications. A flat, freely combinable list rendered
-- as checkboxes; there are deliberately no mutually exclusive groups. See
-- docs/adr/0007-flat-modification-list.md. Free-text modifications live in
-- order_item.note instead.
CREATE TABLE menu_item_modification (
    id                 uuid        PRIMARY KEY,
    menu_item_id       uuid        NOT NULL REFERENCES menu_item (id) ON DELETE CASCADE,
    name               text        NOT NULL,
    price_delta_cents  bigint      NOT NULL DEFAULT 0,
    sort_order         integer     NOT NULL DEFAULT 0,
    deleted_at         timestamptz,

    created_at         timestamptz NOT NULL DEFAULT now(),
    created_by         uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT menu_item_modification_name_length
        CHECK (char_length(name) BETWEEN 1 AND 200)
);

-- price_delta_cents may be negative: "without meat" can reasonably cost less.
COMMENT ON COLUMN menu_item_modification.price_delta_cents IS
    'Added to the item price. May be negative.';

CREATE UNIQUE INDEX menu_item_modification_name_key
    ON menu_item_modification (menu_item_id, lower(name))
    WHERE deleted_at IS NULL;

CREATE TRIGGER menu_item_modification_set_updated_at
    BEFORE UPDATE ON menu_item_modification
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Classification. Free tags and the two regulated lists are separate tables so
-- that the UI can filter by each independently: tags include, allergens and
-- additives exclude.

CREATE TABLE menu_item_tag (
    menu_item_id  uuid        NOT NULL REFERENCES menu_item (id) ON DELETE CASCADE,
    tag_id        uuid        NOT NULL REFERENCES tag (id) ON DELETE RESTRICT,

    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    PRIMARY KEY (menu_item_id, tag_id)
);

CREATE INDEX menu_item_tag_tag_idx ON menu_item_tag (tag_id);

CREATE TRIGGER menu_item_tag_set_updated_at
    BEFORE UPDATE ON menu_item_tag
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE menu_item_allergen (
    menu_item_id  uuid        NOT NULL REFERENCES menu_item (id) ON DELETE CASCADE,
    allergen_id   uuid        NOT NULL REFERENCES allergen (id) ON DELETE RESTRICT,

    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    PRIMARY KEY (menu_item_id, allergen_id)
);

CREATE INDEX menu_item_allergen_allergen_idx ON menu_item_allergen (allergen_id);

CREATE TRIGGER menu_item_allergen_set_updated_at
    BEFORE UPDATE ON menu_item_allergen
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE menu_item_additive (
    menu_item_id  uuid        NOT NULL REFERENCES menu_item (id) ON DELETE CASCADE,
    additive_id   uuid        NOT NULL REFERENCES additive (id) ON DELETE RESTRICT,

    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    PRIMARY KEY (menu_item_id, additive_id)
);

CREATE INDEX menu_item_additive_additive_idx ON menu_item_additive (additive_id);

CREATE TRIGGER menu_item_additive_set_updated_at
    BEFORE UPDATE ON menu_item_additive
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
