-- Images live in the database, not on the filesystem, so that pg_dump plus the
-- configuration file is the whole backup. See
-- docs/adr/0008-images-in-the-database.md.
--
-- Uploads are decoded, downscaled and re-encoded before they get here, so the
-- stored bytes are always within the size the layout needs. Re-encoding also
-- strips EXIF metadata, including GPS coordinates, as a side effect.

CREATE TABLE image (
    id                 uuid        PRIMARY KEY,
    media_type         text        NOT NULL,
    width              integer     NOT NULL,
    height             integer     NOT NULL,
    byte_size          integer     NOT NULL,
    data               bytea       NOT NULL,
    thumb_media_type   text        NOT NULL,
    thumb_width        integer     NOT NULL,
    thumb_height       integer     NOT NULL,
    thumb_data         bytea       NOT NULL,
    sha256             bytea       NOT NULL,
    original_filename  text,

    created_at         timestamptz NOT NULL DEFAULT now(),
    created_by         uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         uuid        REFERENCES app_user (id) ON DELETE SET NULL,

    -- Only these three types are accepted, determined by sniffing the content
    -- rather than trusting the extension or the declared Content-Type.
    CONSTRAINT image_media_type_allowed
        CHECK (media_type IN ('image/jpeg', 'image/png', 'image/gif')),
    CONSTRAINT image_thumb_media_type_allowed
        CHECK (thumb_media_type IN ('image/jpeg', 'image/png', 'image/gif')),
    CONSTRAINT image_dimensions_positive
        CHECK (width > 0 AND height > 0 AND thumb_width > 0 AND thumb_height > 0),
    CONSTRAINT image_byte_size_matches
        CHECK (byte_size = octet_length(data)),
    CONSTRAINT image_sha256_length
        CHECK (octet_length(sha256) = 32)
);

COMMENT ON TABLE image IS 'Restaurant logos and menu item photographs, downscaled on upload.';
COMMENT ON COLUMN image.sha256 IS 'Of the stored image. Used to deduplicate uploads and as the ETag.';

-- Deduplication: the same picture uploaded twice is stored once.
CREATE INDEX image_sha256_idx ON image (sha256);

CREATE TRIGGER image_set_updated_at
    BEFORE UPDATE ON image
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
