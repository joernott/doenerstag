-- Every tick is forgotten. There is nowhere else to keep it.
ALTER TABLE order_item DROP COLUMN paid;
