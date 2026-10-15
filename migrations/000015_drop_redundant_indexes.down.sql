CREATE INDEX IF NOT EXISTS api_token_user_idx ON api_token (user_id);
CREATE INDEX IF NOT EXISTS menu_item_restaurant_idx ON menu_item (restaurant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS session_absolute_expires_idx ON session (absolute_expires);
