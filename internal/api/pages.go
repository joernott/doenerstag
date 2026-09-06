package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/htmlsafe"
)

// PageHandlers serve the imprint and the legal notes.
//
// The two pages an operator supplies and nobody else edits. They are the only
// HTML this application stores that it did not write itself, which is why
// everything on the way in goes through the sanitiser and why only the
// administrator may write at all.
type PageHandlers struct {
	Pool *pgxpool.Pool
}

// ContentPageKeys are the keys the schema's CHECK constraint allows.
//
// Listed here as well so that an unknown key is answered as "that does not
// exist" rather than as a constraint violation surfacing as a 500.
var ContentPageKeys = []string{"imprint", "legal_notes"}

// MaxContentPageBytes bounds a snippet.
//
// An imprint is a few paragraphs. The bound exists so that an administrator
// account cannot be used to put a megabyte of anything into a table every
// visitor reads.
const MaxContentPageBytes = 256 * 1024

// Register adds the content page routes.
func (h *PageHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/pages/:key", h.get)
	r.HandleFunc(http.MethodPut, "/pages/:key", h.put)
}

// get serves one snippet as HTML.
//
// Not JSON: docs/04_api.md lists the content pages among the endpoints that
// are not, because what is stored is a fragment of a document and wrapping it
// in a JSON string would only mean unwrapping it again.
func (h *PageHandlers) get(w http.ResponseWriter, r *http.Request) {
	key, err := contentPageKey(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	var html string
	dbErr := h.Pool.QueryRow(r.Context(),
		`SELECT html FROM content_page WHERE key = $1`, key).Scan(&html)
	switch {
	case errors.Is(dbErr, pgx.ErrNoRows):
		// The migration seeds a placeholder for both keys, so this means
		// somebody removed the row rather than that the key is unknown.
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	writeHTML(w, html)
}

// put replaces one snippet. Administrator only.
func (h *PageHandlers) put(w http.ResponseWriter, r *http.Request) {
	key, keyErr := contentPageKey(r)
	if keyErr != nil {
		WriteError(w, r, keyErr)
		return
	}

	principal, authErr := RequireAdmin(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	// The body is HTML, not JSON, so it is read directly -- with a bound, and
	// one byte beyond it, so that a body of exactly the limit is accepted and
	// anything larger is refused rather than truncated into valid-looking HTML.
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, MaxContentPageBytes+1))
	if readErr != nil {
		WriteError(w, r, &Error{Code: CodeMalformedJSON, Cause: readErr})
		return
	}
	if len(raw) > MaxContentPageBytes {
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Field:  "body",
			Detail: "the snippet is larger than this server accepts",
		})
		return
	}

	sanitised := htmlsafe.Sanitise(string(raw))
	if strings.TrimSpace(sanitised) == "" {
		// Either the body was empty or everything in it was disallowed. Both
		// would leave a page with nothing on it, and the second is worth
		// saying out loud rather than storing silently as blank.
		WriteError(w, r, &Error{
			Code:   CodeMissingField,
			Field:  "body",
			Detail: "the snippet is empty once sanitised",
		})
		return
	}

	id, idErr := uuid.NewV7()
	if idErr != nil {
		WriteError(w, r, &Error{Code: CodeInternal, Cause: idErr})
		return
	}

	_, dbErr := h.Pool.Exec(r.Context(), `
		INSERT INTO content_page (id, key, html, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (key) DO UPDATE
		SET html = EXCLUDED.html, updated_by = EXCLUDED.updated_by`,
		id, key, sanitised, principal.User.ID)
	if dbErr != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	// The sanitised text is returned rather than what was sent, so a caller can
	// see what was actually kept.
	writeHTML(w, sanitised)
}

// contentPageKey reads and checks the key in the path.
func contentPageKey(r *http.Request) (string, *Error) {
	key := Param(r, "key")
	for _, known := range ContentPageKeys {
		if key == known {
			return key, nil
		}
	}
	return "", &Error{Code: CodeNotFound}
}

func writeHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The snippet is sanitised on the way in, but a browser that fetches this
	// directly should still not be talked into sniffing it as something else.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.WriteString(w, html)
}
