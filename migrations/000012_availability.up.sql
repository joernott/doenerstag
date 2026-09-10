-- When a dish can be had, which is not the same as whether it exists.
--
-- `menu_item.available` is a switch somebody flips: sold out today, back
-- tomorrow. This is the standing rule beside it -- the pasta is made from
-- Friday to Sunday between five and ten, and no amount of wanting it on a
-- Tuesday changes that. The restaurant page shows the whole menu either way,
-- because it is the menu; the order page shows what the kitchen will actually
-- make at the time the food is being fetched.
--
-- A filter is one row and one rule, with a name a person chose: "Mittagsmenü",
-- "Weihnachtsessen", "Fri-Sun after 5". Its parts are ANDed -- a filter naming
-- weekdays and a time means those days and only during those hours. Any part
-- left null is "no opinion": a filter with only a time applies on every day.
--
-- The rules for combining filters are in docs/03_data_model.md and are
-- deliberately asymmetric: several filters on one element are alternatives (a
-- Monday filter and a Friday filter make a dish available on both), while a
-- category's filters and an item's must both be satisfied. That means a
-- Monday-only category containing a Wednesday-only item hides that item
-- always. It is what the operator asked for and it is what "the category is
-- only served on Mondays" has to mean.

CREATE TABLE availability_filter (
    id             uuid        PRIMARY KEY,
    restaurant_id  uuid        NOT NULL REFERENCES restaurant (id) ON DELETE CASCADE,
    name           text        NOT NULL,

    -- One calendar date, for a menu that exists on one day. Several dates are
    -- several filters, which is what attaching more than one is for.
    on_date        date,

    -- ISO-8601 numbering, 1 is Monday, as everywhere else in this schema. An
    -- empty array means every day; it is stored empty rather than null so that
    -- reading it never has to distinguish "none" from "unset".
    weekdays       smallint[]  NOT NULL DEFAULT '{}',

    -- Both or neither. end_time < start_time crosses midnight, exactly as
    -- opening_hours allows, because a kitchen that serves from 22:00 to 02:00
    -- is a real kitchen.
    start_time     time,
    end_time       time,

    sort_order     integer     NOT NULL DEFAULT 0,

    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT availability_filter_name_length
        CHECK (char_length(name) BETWEEN 1 AND 200),

    CONSTRAINT availability_filter_weekdays_are_days
        CHECK (weekdays <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[]),

    CONSTRAINT availability_filter_times_together
        CHECK ((start_time IS NULL) = (end_time IS NULL)),

    -- A zero-length window is a typo, the same judgement opening_hours makes.
    CONSTRAINT availability_filter_not_zero_length
        CHECK (start_time IS NULL OR start_time <> end_time),

    -- A filter that says nothing matches everything, which is a row that does
    -- no work and reads as though it does.
    CONSTRAINT availability_filter_says_something
        CHECK (on_date IS NOT NULL
               OR cardinality(weekdays) > 0
               OR start_time IS NOT NULL)
);

COMMENT ON TABLE availability_filter IS
    'A named, reusable rule about when food can be had. Tested against the order''s fulfilment time.';
COMMENT ON COLUMN availability_filter.weekdays IS
    'ISO days, 1 = Monday. Empty means every day.';

CREATE TRIGGER availability_filter_set_updated_at
    BEFORE UPDATE ON availability_filter
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX availability_filter_restaurant_idx
    ON availability_filter (restaurant_id, sort_order);

-- What each filter is attached to. Two tables rather than one with a nullable
-- pair of columns: a row that points at a category and an item at once would
-- be meaningless, and the constraint saying so is harder to read than two
-- tables that cannot express it.
CREATE TABLE menu_category_availability (
    category_id  uuid        NOT NULL REFERENCES menu_category (id) ON DELETE CASCADE,
    filter_id    uuid        NOT NULL REFERENCES availability_filter (id) ON DELETE CASCADE,

    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    PRIMARY KEY (category_id, filter_id)
);

CREATE TRIGGER menu_category_availability_set_updated_at
    BEFORE UPDATE ON menu_category_availability
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE menu_item_availability (
    menu_item_id uuid        NOT NULL REFERENCES menu_item (id) ON DELETE CASCADE,
    filter_id    uuid        NOT NULL REFERENCES availability_filter (id) ON DELETE CASCADE,

    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    PRIMARY KEY (menu_item_id, filter_id)
);

CREATE TRIGGER menu_item_availability_set_updated_at
    BEFORE UPDATE ON menu_item_availability
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Both are read the other way round as well -- "which categories use this
-- filter" -- when a filter is about to be deleted.
CREATE INDEX menu_category_availability_filter_idx
    ON menu_category_availability (filter_id);
CREATE INDEX menu_item_availability_filter_idx
    ON menu_item_availability (filter_id);
