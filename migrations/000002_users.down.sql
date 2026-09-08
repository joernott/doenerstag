DROP TABLE IF EXISTS api_token;
DROP TABLE IF EXISTS session;

-- The delete trigger would otherwise refuse to let the placeholder and the
-- administrator go when the table is emptied.
DROP TRIGGER IF EXISTS app_user_protect_delete ON app_user;
DROP TABLE IF EXISTS app_user;
DROP FUNCTION IF EXISTS prevent_protected_user_delete();
