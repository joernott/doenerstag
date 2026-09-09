package api

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/version"
)

// SystemHandlers serves the operational endpoints from docs/04_api.md.
//
// All three are unauthenticated. They expose counts and versions, never
// personal data, and requiring a credential would make them harder to wire into
// a monitoring system than the information is worth.
type SystemHandlers struct {
	Pool *pgxpool.Pool

	// Shutdown begins a graceful shutdown. Nil disables the endpoint, which is
	// what the tests use.
	Shutdown func()

	// Swagger says whether /tools/swagger is served, and is reported by the
	// version endpoint.
	//
	// The frontend's menu hides the API documentation entry when it is not
	// (docs/06_ui_ux.md), and nothing else can tell it: --no-swagger omits the
	// route rather than answering 404, and an omitted route falls through to
	// the SPA fallback, which answers every path with the application shell.
	// So the server has to say so, and this is the endpoint that already
	// answers "what is this server".
	Swagger bool

	// Retention is --retention, the age past which an expired order is removed
	// by the cleanup endpoint. Zero would delete everything, so a zero value is
	// treated as the documented default rather than obeyed.
	Retention time.Duration

	// MaxImageSize is --max-image-size in bytes, reported for the same reason:
	// the upload control tells a person their photograph is too large before
	// it is sent, and the limit is an operator's decision.
	MaxImageSize int64
}

// Register adds the system routes to a router.
func (h *SystemHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/health", h.health)
	r.HandleFunc(http.MethodGet, "/metrics", h.metrics)
	r.HandleFunc(http.MethodGet, "/version", h.version)
	r.HandleFunc(http.MethodPost, "/cleanup", h.cleanupHandler)
	if h.Shutdown != nil {
		r.HandleFunc(http.MethodPost, "/shutdown", h.shutdownHandler)
	}
}

type healthBody struct {
	Status string `json:"status"`
}

// health reports whether the database answers.
//
// It is what a monitor polls, so it does the smallest possible round trip
// rather than a representative query: the question is "is the connection up",
// not "is the schema right".
func (h *SystemHandlers) health(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil || h.Pool.Ping(r.Context()) != nil {
		_ = WriteJSON(w, http.StatusServiceUnavailable, healthBody{Status: "error"})
		return
	}
	_ = WriteJSON(w, http.StatusOK, healthBody{Status: "ok"})
}

// metricsBody is the shape docs/04_api.md documents.
//
// Plain JSON rather than the Prometheus text format. If scraping is ever
// wanted it belongs behind a separate path, so this one stays stable.
type metricsBody struct {
	Restaurants       int   `json:"restaurants"`
	MenuItems         int   `json:"menu_items"`
	OrdersActive      int   `json:"orders_active"`
	OrdersExpired     int   `json:"orders_expired"`
	Users             int   `json:"users"`
	DBConnectionsOpen int32 `json:"db_connections_open"`
	DBConnectionsIdle int32 `json:"db_connections_idle"`
	DBConnectionsMax  int32 `json:"db_connections_max"`
}

func (h *SystemHandlers) metrics(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable})
		return
	}

	body, err := h.collectMetrics(r.Context())
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}
	_ = WriteJSON(w, http.StatusOK, body)
}

// collectMetrics reads every counter in one round trip.
//
// Six separate queries would be six chances for the numbers to disagree with
// each other, and this is read often enough for that to matter.
func (h *SystemHandlers) collectMetrics(ctx context.Context) (metricsBody, error) {
	var body metricsBody

	err := h.Pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM restaurant WHERE deleted_at IS NULL),
			(SELECT count(*) FROM menu_item WHERE deleted_at IS NULL),
			(SELECT count(*) FROM food_order WHERE deadline_at > now()),
			(SELECT count(*) FROM food_order WHERE deadline_at <= now()),
			(SELECT count(*) FROM app_user)`).
		Scan(&body.Restaurants, &body.MenuItems,
			&body.OrdersActive, &body.OrdersExpired, &body.Users)
	if err != nil {
		return metricsBody{}, err
	}

	stats := db.PoolStats(h.Pool)
	body.DBConnectionsOpen = stats.Open
	body.DBConnectionsIdle = stats.Idle
	body.DBConnectionsMax = stats.Max
	return body, nil
}

type versionBody struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuildDate    string `json:"build_date"`
	Swagger      bool   `json:"swagger"`
	MaxImageSize int64  `json:"max_image_size"`
}

// version reports what the binary is.
//
// The installed schema version comes from the database and is reported by the
// version verb; this endpoint answers even when the database does not, which is
// what makes it useful during an incident.
func (h *SystemHandlers) version(w http.ResponseWriter, _ *http.Request) {
	_ = WriteJSON(w, http.StatusOK, versionBody{
		Version:      version.Version(),
		Commit:       version.Commit(),
		BuildDate:    version.BuildDate(),
		Swagger:      h.Swagger,
		MaxImageSize: h.MaxImageSize,
	})
}

// shutdownHandler begins a graceful shutdown.
//
// Administrator only, per the last row of the matrix in
// docs/05_auth_and_permissions.md. The check lives here rather than in a
// wrapper because this is the one route whose failure mode is the whole
// application: it was written in sprint 3 with a comment promising that sprint
// 5 would wrap it in the administrator check, sprint 5 wrapped every other
// route and not this one, and for nine sprints an anonymous POST could stop the
// server. Anonymous requests are deliberately exempt from the CSRF check, so
// nothing else stood in the way.
//
// It responds before shutting down, so the administrator sees a confirmation
// rather than a dropped connection.
func (h *SystemHandlers) shutdownHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := RequireAdmin(r); err != nil {
		WriteError(w, r, err)
		return
	}

	_ = WriteJSON(w, http.StatusAccepted, map[string]string{
		"status": "shutting down",
	})

	// Flush the response before the server stops accepting connections.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	go h.Shutdown()
}

// cleanupBody is what the cleanup endpoint reports.
type cleanupBody struct {
	Removed map[string]int64 `json:"removed"`
	Total   int64            `json:"total"`
}

// cleanupHandler runs the retention cleanup on demand.
//
// The same work the cleanup verb does from cron, offered to the administrator
// who is looking at a list of expired orders and would rather not wait until
// 03:17 for them to go. Administrator only: it deletes, and the rest of the
// system endpoints are unauthenticated precisely because they do not.
//
// Synchronous, unlike shutdown. The caller wants the number, the work is a
// handful of DELETEs bounded by the retention period, and an endpoint that
// answered "started" would leave the page with nothing to say.
func (h *SystemHandlers) cleanupHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := RequireAdmin(r); err != nil {
		WriteError(w, r, err)
		return
	}

	retention := h.Retention
	if retention <= 0 {
		// A zero retention would treat every order as expired. A handler that
		// was wired up without this value must not delete the database.
		retention = DefaultRetention
	}

	report, err := db.Cleanup(r.Context(), h.Pool, retention, time.Now(), false)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	body := cleanupBody{Removed: map[string]int64{}}
	for _, step := range report.Steps {
		if step.Count > 0 {
			body.Removed[step.Object] = step.Count
		}
	}
	body.Total = report.Total()
	_ = WriteJSON(w, http.StatusOK, body)
}

// DefaultRetention is the fallback for a SystemHandlers built without one.
//
// It matches the documented default of --retention. The cleanup endpoint reads
// it rather than trusting a zero, because zero means "everything is expired".
const DefaultRetention = 14 * 24 * time.Hour
