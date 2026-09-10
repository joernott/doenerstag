-- Who fetches the food and who collects the money become accounts.
--
-- They were free text, which is what you write when the point is only to
-- display a name. It stopped being only that: task 17.8 offers a signed-in
-- visitor a "Me!" button, and a button that writes a display name into a text
-- column cannot tell whether the person who pressed it is the person already
-- named there. A reference can.
--
-- ON DELETE SET NULL rather than the placeholder account the creator uses: an
-- order whose creator is gone still has to belong to somebody so that an
-- administrator can act on it, but an order whose collector is gone simply has
-- no collector, which is the truth and is also a state the column already
-- allowed.

ALTER TABLE food_order
    ADD COLUMN money_collector_id uuid REFERENCES app_user (id) ON DELETE SET NULL,
    ADD COLUMN pickup_person_id   uuid REFERENCES app_user (id) ON DELETE SET NULL;

-- What was typed, matched against what accounts are called.
--
-- Exact and case-insensitive, against the display name first and the user name
-- second, and only where exactly one account matches: two colleagues called
-- "Chris" are a guess, and a wrong guess here names the wrong person as the one
-- holding the money. Anything not matched becomes no reference at all, which is
-- the same thing an empty column already meant. The text is dropped below, so
-- this is the one chance to keep it; that is deliberate, because a column of
-- names that may or may not be an account is exactly the state being left.
-- The predicate appears twice in each statement, once to count the matches and
-- once to take the one there is. That is the price of "only if there is exactly
-- one" as a scalar subquery, and it is cheaper than the alternatives at a scale
-- of one row per order.
UPDATE food_order o
SET money_collector_id = (
    SELECT u.id FROM app_user u
    WHERE lower(coalesce(u.display_name, u.name)) = lower(btrim(o.money_collector))
       OR lower(u.name) = lower(btrim(o.money_collector))
    LIMIT 1
)
WHERE o.money_collector IS NOT NULL
  AND btrim(o.money_collector) <> ''
  AND (
    SELECT count(*) FROM app_user u
    WHERE lower(coalesce(u.display_name, u.name)) = lower(btrim(o.money_collector))
       OR lower(u.name) = lower(btrim(o.money_collector))
  ) = 1;

UPDATE food_order o
SET pickup_person_id = (
    SELECT u.id FROM app_user u
    WHERE lower(coalesce(u.display_name, u.name)) = lower(btrim(o.pickup_person))
       OR lower(u.name) = lower(btrim(o.pickup_person))
    LIMIT 1
)
WHERE o.pickup_person IS NOT NULL
  AND btrim(o.pickup_person) <> ''
  AND (
    SELECT count(*) FROM app_user u
    WHERE lower(coalesce(u.display_name, u.name)) = lower(btrim(o.pickup_person))
       OR lower(u.name) = lower(btrim(o.pickup_person))
  ) = 1;

ALTER TABLE food_order
    DROP CONSTRAINT food_order_money_collector_length,
    DROP CONSTRAINT food_order_pickup_person_length,
    DROP COLUMN money_collector,
    DROP COLUMN pickup_person;

COMMENT ON COLUMN food_order.money_collector_id IS
    'The account collecting payment, or null. Was free text until migration 9.';
COMMENT ON COLUMN food_order.pickup_person_id IS
    'The account fetching the food, or null. Was free text until migration 9.';

-- Both are read on every order page and neither is selected on, so no index:
-- the join is by primary key from the order side. The foreign keys do mean a
-- user deletion scans food_order, which is a table with one row per order and
-- not a table that grows with use.
