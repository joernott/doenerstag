-- Back to the single-column reference. Nothing is lost: every row that
-- satisfies the composite key satisfies this one.

ALTER TABLE menu_item
    DROP CONSTRAINT menu_item_category_same_restaurant;

ALTER TABLE menu_item
    ADD CONSTRAINT menu_item_category_id_fkey
        FOREIGN KEY (category_id) REFERENCES menu_category (id) ON DELETE SET NULL;

ALTER TABLE menu_category
    DROP CONSTRAINT menu_category_restaurant_id_key;
