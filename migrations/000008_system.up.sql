-- The imprint and legal notes pages, and the applied-version history.

CREATE TABLE content_page (
    id          uuid        PRIMARY KEY,
    key         text        NOT NULL UNIQUE,
    html        text        NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),
    created_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT content_page_key_known
        CHECK (key IN ('imprint', 'legal_notes'))
);

COMMENT ON TABLE content_page IS
    'HTML snippets for the imprint and legal notes pages, supplied by the operator.';
COMMENT ON COLUMN content_page.html IS
    'Sanitised on write with an allow-list. Never a whole document: no html, head or body element.';

CREATE TRIGGER content_page_set_updated_at
    BEFORE UPDATE ON content_page
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The rows are created by install from files the operator supplies. Placeholder
-- content is seeded here so that the pages resolve on a database that has been
-- migrated but not yet installed into, and so that the API never has to handle
-- a missing row.
INSERT INTO content_page (id, key, html) VALUES
    ('00000000-0000-7000-8000-000500000001', 'imprint',
     '<p>No imprint has been configured for this installation.</p>'),
    ('00000000-0000-7000-8000-000500000002', 'legal_notes',
     '<p>No legal notes have been configured for this installation.</p>');


-- One row is appended by every install and update run. The version verb and the
-- version page report the newest.
--
-- This is the application's own history. golang-migrate keeps its separate
-- schema_migrations table, which the application never writes to directly.
CREATE TABLE app_version (
    id              uuid        PRIMARY KEY,
    major           integer     NOT NULL,
    minor           integer     NOT NULL,
    patch           integer     NOT NULL,
    schema_version  bigint      NOT NULL,
    applied_at      timestamptz NOT NULL DEFAULT now(),

    created_at      timestamptz NOT NULL DEFAULT now(),
    created_by      uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    updated_by      uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    CONSTRAINT app_version_semver_not_negative
        CHECK (major >= 0 AND minor >= 0 AND patch >= 0),
    CONSTRAINT app_version_schema_version_positive
        CHECK (schema_version > 0)
);

COMMENT ON COLUMN app_version.schema_version IS
    'The golang-migrate version applied by this release.';

CREATE INDEX app_version_applied_at_idx ON app_version (applied_at DESC);

CREATE TRIGGER app_version_set_updated_at
    BEFORE UPDATE ON app_version
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
