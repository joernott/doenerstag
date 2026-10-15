-- Three indexes that nothing needs (20.15).
--
-- api_token_user_idx (user_id) is the leading column of the unique
-- (user_id, name) constraint's index, which serves every lookup by user.
--
-- menu_item_restaurant_idx (restaurant_id) WHERE deleted_at IS NULL is the
-- leading column of menu_item_name_key (restaurant_id, lower(name)), which has
-- the same predicate.
--
-- session_absolute_expires_idx indexes a table holding at most one row per
-- account, and the one query filtering on the column ORs it with last_seen_at,
-- which an index on one of them does not serve.
--
-- Each costs a little on every write and says, to somebody reading the schema,
-- that a query depends on it. None does.

DROP INDEX IF EXISTS api_token_user_idx;
DROP INDEX IF EXISTS menu_item_restaurant_idx;
DROP INDEX IF EXISTS session_absolute_expires_idx;
