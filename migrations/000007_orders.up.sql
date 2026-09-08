-- Orders and their items, per docs/03_data_model.md.
--
-- "order" is a reserved word in SQL, and already means something else in this
-- schema besides, so the table is food_order.

CREATE TABLE food_order (
    id                     uuid        PRIMARY KEY,
    -- ON DELETE SET DEFAULT hands an order whose creator deleted their account
    -- to the placeholder, so the order survives and the administrator can still
    -- remove it.
    creator_id             uuid        NOT NULL
                                       DEFAULT '00000000-0000-7000-8000-000000000000'
                                       REFERENCES app_user (id) ON DELETE SET DEFAULT,
    restaurant_id          uuid        NOT NULL REFERENCES restaurant (id) ON DELETE RESTRICT,
    fulfilment             text        NOT NULL,
    fulfilment_at          timestamptz NOT NULL,
    deadline_at            timestamptz NOT NULL,
    money_collector        text,
    pickup_person          text,
    currency_code          char(3)     NOT NULL REFERENCES currency (code) ON DELETE RESTRICT,
    min_order_value_cents  bigint,
    delivery_fee_cents     bigint,

    created_at             timestamptz NOT NULL DEFAULT now(),
    created_by             uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at             timestamptz NOT NULL DEFAULT now(),
    updated_by             uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT food_order_fulfilment_known
        CHECK (fulfilment IN ('pickup', 'delivery')),

    -- The deadline must fall strictly before the food arrives. Enforced here as
    -- well as in the frontend, because the API is documented for third-party
    -- use and cannot rely on the frontend having checked.
    CONSTRAINT food_order_deadline_before_fulfilment
        CHECK (deadline_at < fulfilment_at),

    CONSTRAINT food_order_min_order_value_not_negative
        CHECK (min_order_value_cents IS NULL OR min_order_value_cents >= 0),
    CONSTRAINT food_order_delivery_fee_not_negative
        CHECK (delivery_fee_cents IS NULL OR delivery_fee_cents >= 0),
    CONSTRAINT food_order_money_collector_length
        CHECK (money_collector IS NULL OR char_length(money_collector) <= 200),
    CONSTRAINT food_order_pickup_person_length
        CHECK (pickup_person IS NULL OR char_length(pickup_person) <= 200)
);

COMMENT ON TABLE food_order IS
    'One group order at one restaurant. Named food_order because "order" is a reserved word.';
COMMENT ON COLUMN food_order.deadline_at IS
    'Last moment items may be added or changed. An order is active while now() is before it.';
COMMENT ON COLUMN food_order.currency_code IS
    'Copied from the restaurant when it was chosen. A later change to the restaurant does not alter existing orders.';

-- There is no title column and no status column. Both are derived: the title
-- from the restaurant name and the fulfilment time, the status from whether
-- deadline_at has passed.

-- Used by the overview, which sorts active before expired, and by the cleanup
-- verb, which selects on it.
CREATE INDEX food_order_deadline_idx ON food_order (deadline_at);
CREATE INDEX food_order_restaurant_idx ON food_order (restaurant_id);
CREATE INDEX food_order_creator_idx ON food_order (creator_id);

CREATE TRIGGER food_order_set_updated_at
    BEFORE UPDATE ON food_order
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE order_item (
    id                uuid        PRIMARY KEY,
    order_id          uuid        NOT NULL REFERENCES food_order (id) ON DELETE CASCADE,
    user_id           uuid        NOT NULL
                                  DEFAULT '00000000-0000-7000-8000-000000000000'
                                  REFERENCES app_user (id) ON DELETE SET DEFAULT,
    menu_item_id      uuid        NOT NULL REFERENCES menu_item (id) ON DELETE RESTRICT,
    quantity          integer     NOT NULL,
    item_name         text        NOT NULL,
    unit_price_cents  bigint      NOT NULL,
    note              text,

    created_at        timestamptz NOT NULL DEFAULT now(),
    created_by        uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    updated_by        uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT order_item_quantity_positive
        CHECK (quantity >= 1),
    CONSTRAINT order_item_unit_price_not_negative
        CHECK (unit_price_cents >= 0),
    CONSTRAINT order_item_name_length
        CHECK (char_length(item_name) BETWEEN 1 AND 200),
    CONSTRAINT order_item_note_length
        CHECK (note IS NULL OR char_length(note) <= 500)
);

-- item_name and unit_price_cents are snapshots taken when the item was ordered,
-- and are authoritative for display and arithmetic. menu_item_id is kept only
-- for grouping in the summary and for the "still on the menu?" check; it is
-- never read for a name or a price. See
-- docs/adr/0009-snapshot-prices-on-order-items.md.
COMMENT ON COLUMN order_item.item_name IS 'Snapshot of menu_item.name at ordering time.';
COMMENT ON COLUMN order_item.unit_price_cents IS 'Snapshot of menu_item.price_cents at ordering time.';
COMMENT ON COLUMN order_item.note IS 'Free-text modification.';

-- Several rows for the same order, user and menu item are allowed: one with
-- onions and one without is two lines, not a conflict. There is deliberately no
-- unique constraint on that triple.

CREATE INDEX order_item_order_idx ON order_item (order_id);
CREATE INDEX order_item_user_idx ON order_item (user_id);
-- Needed by cleanup, to find soft-deleted menu items nothing references.
CREATE INDEX order_item_menu_item_idx ON order_item (menu_item_id);

CREATE TRIGGER order_item_set_updated_at
    BEFORE UPDATE ON order_item
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE order_item_modification (
    id                 uuid        PRIMARY KEY,
    order_item_id      uuid        NOT NULL REFERENCES order_item (id) ON DELETE CASCADE,
    -- Nullable, so removing a modification from a menu cannot damage a
    -- historical order. The snapshot below is what actually matters.
    modification_id    uuid        REFERENCES menu_item_modification (id) ON DELETE SET NULL,
    name               text        NOT NULL,
    price_delta_cents  bigint      NOT NULL DEFAULT 0,

    created_at         timestamptz NOT NULL DEFAULT now(),
    created_by         uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT order_item_modification_name_length
        CHECK (char_length(name) BETWEEN 1 AND 200)
);

COMMENT ON TABLE order_item_modification IS
    'Predefined modifications selected for an order item, with their name and price snapshotted.';

CREATE INDEX order_item_modification_item_idx ON order_item_modification (order_item_id);
CREATE INDEX order_item_modification_source_idx ON order_item_modification (modification_id);

CREATE TRIGGER order_item_modification_set_updated_at
    BEFORE UPDATE ON order_item_modification
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
