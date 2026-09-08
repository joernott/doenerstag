package api

import (
	"context"
	"net/http"

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
}

// Register adds the system routes to a router.
func (h *SystemHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/health", h.health)
	r.HandleFunc(http.MethodGet, "/metrics", h.metrics)
	r.HandleFunc(http.MethodGet, "/version", h.version)
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
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	Swagger   bool   `json:"swagger"`
}

// version reports what the binary is.
//
// The installed schema version comes from the database and is reported by the
// version verb; this endpoint answers even when the database does not, which is
// what makes it useful during an incident.
func (h *SystemHandlers) version(w http.ResponseWriter, _ *http.Request) {
	_ = WriteJSON(w, http.StatusOK, versionBody{
		Version:   version.Version(),
		Commit:    version.Commit(),
		BuildDate: version.BuildDate(),
		Swagger:   h.Swagger,
	})
}

// shutdownHandler begins a graceful shutdown.
//
// It responds before shutting down, so the administrator sees a confirmation
// rather than a dropped connection. Authorisation is the caller's
// responsibility: sprint 5 wraps this route in the administrator check, and
// until then the route is only registered when a Shutdown function is supplied.
func (h *SystemHandlers) shutdownHandler(w http.ResponseWriter, _ *http.Request) {
	_ = WriteJSON(w, http.StatusAccepted, map[string]string{
		"status": "shutting down",
	})

	// Flush the response before the server stops accepting connections.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	go h.Shutdown()
}
