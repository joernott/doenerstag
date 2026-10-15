-- updated_at moves when a row's data changes, not when only its audit columns do.
--
-- created_by and updated_by reference app_user ON DELETE SET NULL, and
-- PostgreSQL carries out SET NULL as an UPDATE of the referencing row, which
-- fires that row's BEFORE UPDATE triggers. So deleting an account rewrote
-- updated_at on every restaurant, menu item and order it had ever created or
-- edited -- "last modified" moving for a reason nobody would guess, and
-- recording when the account was deleted on rows that have nothing to do with
-- it (20.13).
--
-- The rule: if nothing but updated_at, updated_by and created_by differs, the
-- old timestamp stands. That also keeps a no-op update -- the same values
-- written back -- from claiming a change.

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (to_jsonb(NEW) - '{updated_at,updated_by,created_by}'::text[])
       = (to_jsonb(OLD) - '{updated_at,updated_by,created_by}'::text[]) THEN
        NEW.updated_at := OLD.updated_at;
    ELSE
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION set_updated_at() IS
    'Trigger function maintaining the updated_at audit column: now() when the row''s data changed, the old value when only audit columns did. Attached by every table.';
