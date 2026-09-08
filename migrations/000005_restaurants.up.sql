-- Restaurants, their contacts and their opening hours, per docs/03_data_model.md.

CREATE TABLE restaurant (
    id                     uuid        PRIMARY KEY,
    name                   text        NOT NULL,
    logo_image_id          uuid        REFERENCES image (id) ON DELETE SET NULL,
    currency_code          char(3)     NOT NULL REFERENCES currency (code) ON DELETE RESTRICT,
    min_order_value_cents  bigint,
    delivery_fee_cents     bigint,
    notes                  text,
    deleted_at             timestamptz,

    created_at             timestamptz NOT NULL DEFAULT now(),
    created_by             uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at             timestamptz NOT NULL DEFAULT now(),
    updated_by             uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT restaurant_name_length
        CHECK (char_length(name) BETWEEN 1 AND 200),
    CONSTRAINT restaurant_min_order_value_not_negative
        CHECK (min_order_value_cents IS NULL OR min_order_value_cents >= 0),
    CONSTRAINT restaurant_delivery_fee_not_negative
        CHECK (delivery_fee_cents IS NULL OR delivery_fee_cents >= 0)
);

COMMENT ON TABLE restaurant IS 'Restaurants orders can be placed at. Any logged-in user may add and edit one.';
COMMENT ON COLUMN restaurant.currency_code IS 'Applies to the whole menu. There is no per-item currency.';
COMMENT ON COLUMN restaurant.deleted_at IS 'Soft delete. Rows are removed physically by the cleanup verb once nothing references them.';

-- Uniqueness applies among the living. A soft-deleted restaurant must not block
-- someone re-adding one with the same name.
CREATE UNIQUE INDEX restaurant_name_key
    ON restaurant (lower(name))
    WHERE deleted_at IS NULL;

CREATE TRIGGER restaurant_set_updated_at
    BEFORE UPDATE ON restaurant
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- The pickup address is a contact of type 'address'; there is no separate
-- address column.
CREATE TABLE restaurant_contact (
    id               uuid        PRIMARY KEY,
    restaurant_id    uuid        NOT NULL REFERENCES restaurant (id) ON DELETE CASCADE,
    contact_type_id  uuid        NOT NULL REFERENCES contact_type (id) ON DELETE RESTRICT,
    value            text        NOT NULL,
    label            text,
    sort_order       integer     NOT NULL DEFAULT 0,

    created_at       timestamptz NOT NULL DEFAULT now(),
    created_by       uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    updated_by       uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT restaurant_contact_value_length
        CHECK (char_length(value) BETWEEN 1 AND 500),
    CONSTRAINT restaurant_contact_label_length
        CHECK (label IS NULL OR char_length(label) BETWEEN 1 AND 100)
);

-- A restaurant needs at least one contact. That is enforced by the application
-- rather than here: a table constraint cannot express "at least one row in
-- another table" without a deferred trigger, and the cost of one would be paid
-- on every insert to buy very little.

CREATE INDEX restaurant_contact_restaurant_idx ON restaurant_contact (restaurant_id);

CREATE TRIGGER restaurant_contact_set_updated_at
    BEFORE UPDATE ON restaurant_contact
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE opening_hours (
    id             uuid        PRIMARY KEY,
    restaurant_id  uuid        NOT NULL REFERENCES restaurant (id) ON DELETE CASCADE,
    day_of_week    smallint    NOT NULL,
    start_time     time        NOT NULL,
    end_time       time        NOT NULL,

    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    -- ISO-8601 numbering: 1 is Monday, 7 is Sunday.
    CONSTRAINT opening_hours_day_of_week_range
        CHECK (day_of_week BETWEEN 1 AND 7),

    -- end_time < start_time is legal and means the period crosses midnight,
    -- as a Friday 22:00-02:00 does. Equal times are not: a zero-length opening
    -- period is a typo, not a restaurant that is never open.
    CONSTRAINT opening_hours_not_zero_length
        CHECK (end_time <> start_time)
);

COMMENT ON TABLE opening_hours IS
    'Local wall-clock opening periods. Several rows per weekday are allowed, for a lunch break.';
COMMENT ON COLUMN opening_hours.day_of_week IS 'ISO-8601: 1 = Monday through 7 = Sunday.';
COMMENT ON COLUMN opening_hours.end_time IS
    'An end earlier than the start means the period crosses midnight.';

CREATE INDEX opening_hours_restaurant_idx ON opening_hours (restaurant_id, day_of_week);

CREATE TRIGGER opening_hours_set_updated_at
    BEFORE UPDATE ON opening_hours
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
