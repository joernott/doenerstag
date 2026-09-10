-- Back to free text, filled in from the accounts that are referenced.
--
-- This is lossy in the direction the up migration was lossy in the other: a
-- name that matched no account was dropped going up and cannot come back, and
-- a reference restored as text is a name rather than a person.

ALTER TABLE food_order
    ADD COLUMN money_collector text,
    ADD COLUMN pickup_person   text;

UPDATE food_order o
SET money_collector = (SELECT coalesce(u.display_name, u.name) FROM app_user u WHERE u.id = o.money_collector_id)
WHERE o.money_collector_id IS NOT NULL;

UPDATE food_order o
SET pickup_person = (SELECT coalesce(u.display_name, u.name) FROM app_user u WHERE u.id = o.pickup_person_id)
WHERE o.pickup_person_id IS NOT NULL;

ALTER TABLE food_order
    ADD CONSTRAINT food_order_money_collector_length
        CHECK (money_collector IS NULL OR char_length(money_collector) <= 200),
    ADD CONSTRAINT food_order_pickup_person_length
        CHECK (pickup_person IS NULL OR char_length(pickup_person) <= 200),
    DROP COLUMN money_collector_id,
    DROP COLUMN pickup_person_id;
