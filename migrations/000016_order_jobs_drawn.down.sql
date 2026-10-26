-- The people picked keep their jobs; only the record that it was by lot goes.
DROP INDEX food_order_draw_due_idx;
ALTER TABLE food_order
    DROP COLUMN money_collector_drawn,
    DROP COLUMN pickup_person_drawn,
    DROP COLUMN jobs_drawn;
