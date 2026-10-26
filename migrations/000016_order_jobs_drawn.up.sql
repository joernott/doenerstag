-- Who was picked by lot (21.3).
--
-- When an order's deadline passes and nobody has said they will fetch the
-- food or collect the money, the server picks one of the people with an item
-- in it and puts them in every job still empty. jobs_drawn records that the
-- draw has been made, whether or not it found anybody, so it happens once: a
-- restart, a second server process or a second tick of the loop finds it done.
--
-- The two *_drawn flags say which of the jobs the draw filled, so the order
-- page can tell somebody picked by lot from somebody who volunteered. They are
-- not cleared when the account goes away: the person column is, and a drawn
-- flag on an empty job says nothing anybody reads.

ALTER TABLE food_order
    ADD COLUMN jobs_drawn            boolean NOT NULL DEFAULT false,
    ADD COLUMN pickup_person_drawn   boolean NOT NULL DEFAULT false,
    ADD COLUMN money_collector_drawn boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN food_order.jobs_drawn IS
    'The draw for empty jobs at the deadline has been made. Once per order.';
COMMENT ON COLUMN food_order.pickup_person_drawn IS
    'pickup_person_id was filled by the draw rather than by a volunteer.';
COMMENT ON COLUMN food_order.money_collector_drawn IS
    'money_collector_id was filled by the draw rather than by a volunteer.';

-- Orders that closed before this migration are not drawn for after the fact:
-- the food was fetched, by somebody, long ago, and naming a stranger for it
-- now would be wrong.
UPDATE food_order SET jobs_drawn = true WHERE deadline_at <= now();

-- What the draw loop asks every few seconds: which orders are past their
-- deadline and not yet drawn. Almost always none, so the index stays tiny.
CREATE INDEX food_order_draw_due_idx ON food_order (deadline_at) WHERE NOT jobs_drawn;
