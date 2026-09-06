package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// imageColumns is the metadata projection. It deliberately excludes data and
// thumb_data: almost every read wants the metadata, and pulling several hundred
// kilobytes of bytea to answer "does this image exist" would be wasteful in a
// way that is easy not to notice.
const imageColumns = `id, media_type, width, height, byte_size,
	thumb_media_type, thumb_width, thumb_height,
	sha256, coalesce(original_filename, ''), created_at`

func scanImage(row pgx.Row) (model.Image, error) {
	var img model.Image
	err := row.Scan(&img.ID, &img.MediaType, &img.Width, &img.Height, &img.ByteSize,
		&img.ThumbMediaType, &img.ThumbWidth, &img.ThumbHeight,
		&img.SHA256, &img.OriginalFilename, &img.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Image{}, ErrNotFound
		}
		return model.Image{}, err
	}
	return img, nil
}

// NewImage is a processed upload ready for storage.
type NewImage struct {
	MediaType string
	Width     int
	Height    int
	Data      []byte

	ThumbMediaType string
	ThumbWidth     int
	ThumbHeight    int
	ThumbData      []byte

	SHA256           []byte
	OriginalFilename string
}

// CreateImage stores an image, or returns the existing one with the same
// content.
//
// Deduplication is by SHA-256 of the stored bytes, so the same picture uploaded
// twice is stored once. It is checked before the insert rather than enforced by
// a unique constraint, because two people uploading the same logo is a normal
// thing to do and should not look like an error to either of them.
//
// The check and the insert race: two simultaneous uploads of the same picture
// can both miss and both insert. That is harmless -- two rows with the same
// content, which cleanup will not merge but nothing depends on -- and the
// alternative, a unique index on sha256, would make the second upload fail
// rather than succeed. Storing a duplicate is the better failure.
func CreateImage(ctx context.Context, q Querier, in NewImage, actor uuid.UUID) (model.Image, error) {
	existing, err := ImageBySHA256(ctx, q, in.SHA256)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return model.Image{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return model.Image{}, fmt.Errorf("generating an image id: %w", err)
	}

	return scanImage(q.QueryRow(ctx, `
		INSERT INTO image (id, media_type, width, height, byte_size, data,
		                   thumb_media_type, thumb_width, thumb_height, thumb_data,
		                   sha256, original_filename, created_by, updated_by)
		-- $5 is cast at both uses: unqualified, PostgreSQL cannot deduce one type
		-- for a parameter that is an octet_length argument in one place and a
		-- bytea column value in another.
		VALUES ($1, $2, $3, $4, octet_length($5::bytea), $5::bytea,
		        $6, $7, $8, $9,
		        $10, nullif($11, ''), $12, $12)
		RETURNING `+imageColumns,
		id, in.MediaType, in.Width, in.Height, in.Data,
		in.ThumbMediaType, in.ThumbWidth, in.ThumbHeight, in.ThumbData,
		in.SHA256, in.OriginalFilename, actor))
}

// ImageByID reads an image's metadata.
func ImageByID(ctx context.Context, q Querier, id uuid.UUID) (model.Image, error) {
	return scanImage(q.QueryRow(ctx,
		`SELECT `+imageColumns+` FROM image WHERE id = $1`, id))
}

// ImageBySHA256 finds an image by the hash of its stored bytes.
func ImageBySHA256(ctx context.Context, q Querier, sum []byte) (model.Image, error) {
	return scanImage(q.QueryRow(ctx,
		`SELECT `+imageColumns+` FROM image WHERE sha256 = $1 LIMIT 1`, sum))
}

// ImageData reads the bytes of an image, and its media type.
//
// Separate from the metadata read on purpose: this is the only query that
// carries the payload, so every call site that transfers hundreds of kilobytes
// is visible as a call to this function.
func ImageData(ctx context.Context, q Querier, id uuid.UUID) (mediaType string, data []byte, err error) {
	err = q.QueryRow(ctx,
		`SELECT media_type, data FROM image WHERE id = $1`, id).Scan(&mediaType, &data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, ErrNotFound
		}
		return "", nil, err
	}
	return mediaType, data, nil
}

// ThumbnailData reads the thumbnail bytes and its media type.
func ThumbnailData(ctx context.Context, q Querier, id uuid.UUID) (mediaType string, data []byte, err error) {
	err = q.QueryRow(ctx,
		`SELECT thumb_media_type, thumb_data FROM image WHERE id = $1`, id).
		Scan(&mediaType, &data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, ErrNotFound
		}
		return "", nil, err
	}
	return mediaType, data, nil
}

// DeleteUnreferencedImages removes images nothing points at.
//
// There is no DELETE /images: an image is referenced by id from two tables, and
// letting a caller remove one would leave a restaurant with a broken logo. The
// cleanup verb calls this instead, which is the whole reason the ADR calls
// orphans impossible in the sense that matters.
func DeleteUnreferencedImages(ctx context.Context, q Querier) (int64, error) {
	tag, err := q.Exec(ctx, `
		DELETE FROM image
		WHERE NOT EXISTS (SELECT 1 FROM restaurant WHERE logo_image_id = image.id)
		  AND NOT EXISTS (SELECT 1 FROM menu_item WHERE image_id = image.id)`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
