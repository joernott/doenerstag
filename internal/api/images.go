package api

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/image"
	"github.com/joernott/doenerstag/internal/model"
)

// ImageFormField is the multipart field the upload is read from.
const ImageFormField = "file"

// MaxOriginalFilenameLength bounds what is kept of the uploaded name.
//
// The name is attacker-controlled, is stored, and is echoed back. It is kept
// only so a user can tell two uploads apart, so a bound well under the column's
// is fine.
const MaxOriginalFilenameLength = 255

// ImageHandlers serves upload and retrieval.
type ImageHandlers struct {
	Pool *pgxpool.Pool

	// MaxUploadBytes is --max-image-size, applied before the body is read into
	// memory.
	MaxUploadBytes int64
}

// Register adds the image routes.
//
// There is no DELETE: an image is referenced by id from restaurants and menu
// items, and letting a caller remove one would leave a broken logo behind.
// Unreferenced images are removed by the cleanup verb.
func (h *ImageHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodPost, "/images", h.upload)
	r.HandleFunc(http.MethodGet, "/images/:id", h.serveImage)
	r.HandleFunc(http.MethodGet, "/images/:id/thumbnail", h.serveThumbnail)
}

type imageBody struct {
	ID        string `json:"id"`
	MediaType string `json:"media_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ByteSize  int    `json:"byte_size"`

	ThumbMediaType string `json:"thumb_media_type"`
	ThumbWidth     int    `json:"thumb_width"`
	ThumbHeight    int    `json:"thumb_height"`

	SHA256           string `json:"sha256"`
	OriginalFilename string `json:"original_filename"`
	CreatedAt        string `json:"created_at"`
}

func publicImage(img model.Image) imageBody {
	return imageBody{
		ID:               img.ID.String(),
		MediaType:        img.MediaType,
		Width:            img.Width,
		Height:           img.Height,
		ByteSize:         img.ByteSize,
		ThumbMediaType:   img.ThumbMediaType,
		ThumbWidth:       img.ThumbWidth,
		ThumbHeight:      img.ThumbHeight,
		SHA256:           hex.EncodeToString(img.SHA256),
		OriginalFilename: img.OriginalFilename,
		CreatedAt:        img.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// upload accepts a multipart file, processes it and stores it.
func (h *ImageHandlers) upload(w http.ResponseWriter, r *http.Request) {
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	// The cap is applied to the request body before anything is read, so an
	// oversized upload costs the bytes it takes to notice rather than the whole
	// file in memory. MaxBytesReader also makes the failure a typed error the
	// handler can recognise, instead of a truncated read that looks like a
	// corrupt image.
	r.Body = http.MaxBytesReader(w, r.Body, h.MaxUploadBytes)

	file, filename, err := h.readUpload(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	processed, procErr := image.Process(file)
	switch {
	case errors.Is(procErr, image.ErrUnsupportedType), errors.Is(procErr, image.ErrDecode):
		// Both answer 1012. From the caller's side "this is not an image I can
		// read" is one problem, whether the bytes were a shell script or a
		// truncated JPEG.
		WriteError(w, r, &Error{
			Code:   CodeImageUnsupportedType,
			Field:  ImageFormField,
			Detail: "the file is not a JPEG, PNG or GIF image",
			Cause:  procErr,
		})
		return
	case errors.Is(procErr, image.ErrEmpty):
		WriteError(w, r, &Error{
			Code:   CodeMissingField,
			Field:  ImageFormField,
			Detail: "the uploaded file is empty",
		})
		return
	case procErr != nil:
		WriteError(w, r, &Error{Code: CodeInternal, Cause: procErr})
		return
	}

	stored, dbErr := db.CreateImage(r.Context(), h.Pool, db.NewImage{
		MediaType:        processed.MediaType,
		Width:            processed.Width,
		Height:           processed.Height,
		Data:             processed.Data,
		ThumbMediaType:   processed.ThumbMediaType,
		ThumbWidth:       processed.ThumbWidth,
		ThumbHeight:      processed.ThumbHeight,
		ThumbData:        processed.ThumbData,
		SHA256:           processed.SHA256,
		OriginalFilename: filename,
	}, principal.User.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	_ = WriteJSON(w, http.StatusCreated, publicImage(stored))
}

// readUpload pulls the file out of the multipart body.
func (h *ImageHandlers) readUpload(r *http.Request) (content []byte, filename string, failure *Error) {
	file, header, err := r.FormFile(ImageFormField)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, "", &Error{
				Code:  CodeImageTooLarge,
				Field: ImageFormField,
				Detail: fmt.Sprintf("the upload may be at most %d bytes",
					h.MaxUploadBytes),
			}
		}
		return nil, "", &Error{
			Code:  CodeMissingField,
			Field: ImageFormField,
			Detail: fmt.Sprintf("send the image as multipart/form-data in the %q field",
				ImageFormField),
		}
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, "", &Error{
				Code:  CodeImageTooLarge,
				Field: ImageFormField,
				Detail: fmt.Sprintf("the upload may be at most %d bytes",
					h.MaxUploadBytes),
			}
		}
		return nil, "", &Error{Code: CodeInternal, Cause: err}
	}

	return data, sanitiseFilename(header.Filename), nil
}

// sanitiseFilename reduces an uploaded name to something safe to store and
// echo back.
//
// Only the base name is kept, so a caller cannot store a path. Control
// characters go, because the value is rendered into HTML and into log lines.
// Nothing here is load-bearing for security -- the name is never used to open
// anything -- but a stored string that is echoed to other users deserves the
// same treatment as any other.
func sanitiseFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || name == ".." {
		return ""
	}

	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)

	if utf8.RuneCountInString(name) > MaxOriginalFilenameLength {
		runes := []rune(name)
		name = string(runes[:MaxOriginalFilenameLength])
	}
	return name
}

func (h *ImageHandlers) serveImage(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, db.ImageData)
}

func (h *ImageHandlers) serveThumbnail(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, db.ThumbnailData)
}

// serve writes image bytes with caching headers.
//
// The ETag is the SHA-256 of the stored image, which is already computed and
// already stored, so revalidation costs one metadata query rather than reading
// the payload. The main image and the thumbnail share it with a suffix: they
// are different resources at different URLs, and an ETag must not collide
// across them.
//
// Cache-Control is immutable because an image's bytes never change -- editing a
// logo means uploading a new image and pointing the restaurant at it, so the
// id and the content are fixed together for the life of the row.
func (h *ImageHandlers) serve(
	w http.ResponseWriter, r *http.Request,
	read func(ctx context.Context, q db.Querier, id uuid.UUID) (string, []byte, error),
) {
	id, parseErr := uuid.Parse(Param(r, "id"))
	if parseErr != nil {
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}

	meta, dbErr := db.ImageByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	etag := imageETag(meta, strings.HasSuffix(r.URL.Path, "/thumbnail"))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	// A matching ETag means the client already has these bytes, so the payload
	// query is skipped entirely -- which is the point of doing the metadata
	// read first.
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	mediaType, data, dbErr := read(r.Context(), h.Pool, id)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	// The bytes are a decoded and re-encoded image, so the type is known
	// exactly; nosniff stops a browser second-guessing it.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

// imageETag builds a strong ETag from the stored hash.
func imageETag(meta model.Image, thumbnail bool) string {
	suffix := ""
	if thumbnail {
		suffix = "-t"
	}
	return `"` + hex.EncodeToString(meta.SHA256) + suffix + `"`
}

// matchesETag implements If-None-Match against one entity tag.
//
// A comma-separated list is compared entry by entry, and "*" matches anything
// that exists, both per RFC 9110. The weak prefix is stripped before comparing,
// because a weak match is good enough for a 304 on a byte-identical resource.
func matchesETag(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}
