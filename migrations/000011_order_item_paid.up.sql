-- Whether an order item has been paid for.
--
-- The application does not handle payment and is not going to
-- (docs/01_overview.md lists it as out of scope). This is not payment: it is a
-- tick against a line on a list, replacing the person with the money doing it
-- on paper or in their head. What settles the debt is a note handed over with
-- the food; this records that it happened.
--
-- Deliberately outside the deadline rule that freezes everything else about an
-- item. An order closes before the food is ordered, and the money changes hands
-- when the food arrives -- so the moment somebody wants to tick this box is
-- always after the deadline. A `paid` flag that could only be set while the
-- order was open would be a flag nobody could ever set.

ALTER TABLE order_item
    ADD COLUMN paid boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN order_item.paid IS
    'Somebody recorded this line as settled. Not a payment; the application takes none.';

-- No index. The only query that reads it is the summary of one order, which
-- already selects every item of that order by order_id and decides this in the
-- application.
