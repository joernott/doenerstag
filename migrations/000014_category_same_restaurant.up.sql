-- A dish's category belongs to the dish's restaurant, and the schema says so.
--
-- menu_item.category_id referenced menu_category (id) alone, so a dish could be
-- filed under another restaurant's category. The API refused that, but only
-- the API: the comment in internal/api/menuitems.go said the foreign key could
-- not prevent it. A foreign key over both columns can (20.14).
--
-- ON DELETE SET NULL (category_id) clears only the category when one is
-- deleted, as the single-column key did; nulling restaurant_id as well would
-- violate its NOT NULL. A column list on SET NULL needs PostgreSQL 15.

-- The key the composite reference points at. id is unique already, so this
-- cannot fail on existing data.
ALTER TABLE menu_category
    ADD CONSTRAINT menu_category_restaurant_id_key UNIQUE (restaurant_id, id);

-- Rows that break the rule could only have come from outside the API. Losing
-- their category is the honest outcome -- the category was never theirs -- and
-- it keeps this migration from refusing to run on an installation that has one.
UPDATE menu_item i
   SET category_id = NULL
  FROM menu_category c
 WHERE c.id = i.category_id
   AND c.restaurant_id <> i.restaurant_id;

ALTER TABLE menu_item
    DROP CONSTRAINT menu_item_category_id_fkey;

ALTER TABLE menu_item
    ADD CONSTRAINT menu_item_category_same_restaurant
        FOREIGN KEY (restaurant_id, category_id)
        REFERENCES menu_category (restaurant_id, id)
        ON DELETE SET NULL (category_id);
