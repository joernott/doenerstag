-- Audit column conventions, per docs/03_data_model.md.
--
-- Every table carries created_at, created_by, updated_at and updated_by.
-- created_by and updated_by reference app_user and are nullable, so that a
-- seeded or system-created row can say "nobody", and so that deleting a user
-- never blocks on a foreign key.
--
-- updated_at is maintained here rather than by every UPDATE statement in the
-- application. A timestamp that depends on each caller remembering to set it
-- is a timestamp that is wrong.

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION set_updated_at() IS
    'Trigger function maintaining the updated_at audit column. Attached by every table.';
