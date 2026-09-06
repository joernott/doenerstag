package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/static"
)

// ServerOptions is everything the server needs to run.
type ServerOptions struct {
	Config *config.Config
	Pool   *pgxpool.Pool
	Logger *zerolog.Logger
	Assets *static.Assets

	// StaticDirWasSet reports whether the operator passed --static-dir
	// explicitly, so that an embedded build can warn it is being ignored.
	StaticDirWasSet bool
}

// Server owns the HTTP listener and its lifecycle.
type Server struct {
	http   *http.Server
	cfg    *config.Config
	logger *zerolog.Logger
	secure bool

	// shutdown receives when the shutdown endpoint is called. Buffered, so the
	// handler never blocks on whoever is running the server.
	shutdown chan struct{}
}

// NewServer builds the server and its routes.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.Config == nil {
		return nil, errors.New("api: no configuration was supplied")
	}
	if opts.Logger == nil {
		return nil, errors.New("api: no logger was supplied")
	}

	cfg := opts.Config
	secure := !cfg.Server.NoHTTPS

	routerOptions := Options{
		CORSOrigins: ParseCORSOrigins(cfg.Server.CORSAllowedOrigins),
	}
	if opts.Assets != nil {
		routerOptions.Assets = opts.Assets.Handler()
		routerOptions.Index = opts.Assets.Index()

		// --no-swagger omits the route entirely rather than answering 404, so
		// an operator who turned it off cannot tell it apart from a path that
		// never existed.
		if !cfg.Server.NoSwagger {
			routerOptions.Swagger = SwaggerHandler(opts.Assets.SwaggerIndex())
		}
	}

	router := NewRouter(routerOptions)
	router.RegisterOpenAPI()

	s := &Server{
		cfg:      cfg,
		logger:   opts.Logger,
		secure:   secure,
		shutdown: make(chan struct{}, 1),
	}

	system := &SystemHandlers{Pool: opts.Pool, Shutdown: s.beginShutdown}
	system.Register(router)

	// The signer is built here rather than per request: an unusable secret must
	// stop the server starting, not fail the first login. StartupChecks has
	// already rejected a short one, so this only fails on a configuration that
	// never reached it -- a test constructing a server directly, say.
	signer, err := auth.NewSigner(cfg.Session.JWTSecret)
	if err != nil {
		return nil, err
	}

	authHandlers := &AuthHandlers{
		Pool:            opts.Pool,
		Signer:          signer,
		AbsoluteTimeout: cfg.Session.AbsoluteTimeout,
		Secure:          secure,
		Logger:          opts.Logger,
	}
	authHandlers.Register(router)

	authenticator := &Authenticator{
		Pool:        opts.Pool,
		Signer:      signer,
		IdleTimeout: cfg.Session.IdleTimeout,
	}

	// The request ID is outermost because every line inside carries it. Recovery
	// sits inside logging so that a panicking request still produces its
	// completion line.
	//
	// Authentication is innermost of the four that always run: it needs the
	// request ID for its error envelope, it must be inside the recovery
	// handler, and it must run before the CSRF check, which asks how the caller
	// authenticated. Everything below it therefore sees a resolved principal.
	handler := Chain(router,
		RequestID(),
		LogRequests(opts.Logger),
		Recover(opts.Logger),
		SecurityHeaders(secure),
		authenticator.Middleware(),
	)

	s.http = &http.Server{
		Addr:              net.JoinHostPort(cfg.Server.BindAddress, strconv.Itoa(cfg.Server.Port)),
		Handler:           handler,
		ReadTimeout:       cfg.Server.HTTPReadTimeout,
		WriteTimeout:      cfg.Server.HTTPWriteTimeout,
		IdleTimeout:       cfg.Server.HTTPIdleTimeout,
		ReadHeaderTimeout: cfg.Server.HTTPReadTimeout,
		ErrorLog:          nil,
	}

	return s, nil
}

// Handler exposes the fully wrapped handler, so a test can drive the whole
// chain through httptest without binding a port.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Addr is the address the server listens on.
func (s *Server) Addr() string { return s.http.Addr }

// StartupChecks are the five things that must hold before the server begins
// listening, per docs/09_configuration.md.
//
// Each failure is fatal and says what to do about it. Finding out now beats
// finding out when the first request arrives.
func StartupChecks(ctx context.Context, opts ServerOptions) error {
	cfg := opts.Config

	// 1. The database must answer.
	if opts.Pool == nil {
		return errors.New("no database connection")
	}
	if err := opts.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("cannot reach the database: %w", err)
	}

	// 2. And be a version we support.
	if err := db.VerifyServerVersion(ctx, opts.Pool); err != nil {
		return err
	}

	// 3. The schema must match what this binary expects, so a forgotten
	//    `doenerstag update` fails loudly rather than producing subtle errors
	//    somewhere else entirely.
	if err := checkSchemaVersion(ctx, opts.Pool); err != nil {
		return err
	}

	// 4. TLS material must be readable, unless running plain.
	if !cfg.Server.NoHTTPS {
		if err := checkTLSMaterial(cfg); err != nil {
			return err
		}
	}

	// 5. A session signing secret must exist and be long enough.
	if len(cfg.Session.JWTSecret) < MinJWTSecretLength {
		return fmt.Errorf(
			"the session signing secret is %d characters; at least %d are required.\n"+
				"Run doenerstag install to generate one, or set DOENER_JWT_SECRET",
			len(cfg.Session.JWTSecret), MinJWTSecretLength)
	}

	return nil
}

// MinJWTSecretLength is the shortest session signing secret the server accepts.
const MinJWTSecretLength = 32

func checkSchemaVersion(ctx context.Context, pool *pgxpool.Pool) error {
	expected, err := db.LatestVersion()
	if err != nil {
		return err
	}

	var applied int64
	err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&applied)
	if err != nil {
		return fmt.Errorf(
			"cannot read the schema version: %w\nRun doenerstag install first", err)
	}

	if uint(applied) != expected { //nolint:gosec // a migration version is small and positive
		return fmt.Errorf(
			"the database schema is at version %d but this binary expects %d.\n"+
				"Run doenerstag update to bring the database forward", applied, expected)
	}
	return nil
}

func checkTLSMaterial(cfg *config.Config) error {
	for _, file := range []struct{ what, path string }{
		{"certificate", cfg.Server.TLSCert},
		{"private key", cfg.Server.TLSKey},
	} {
		if file.path == "" {
			return fmt.Errorf("no TLS %s was configured; use --no-https to serve plain HTTP", file.what)
		}
		if _, err := os.Stat(file.path); err != nil {
			return fmt.Errorf(
				"cannot read the TLS %s at %s: %w\n"+
					"Point --tls-%s at a readable file, or use --no-https",
				file.what, file.path, err, tlsFlagFor(file.what))
		}
	}
	return nil
}

func tlsFlagFor(what string) string {
	if what == "certificate" {
		return "cert"
	}
	return "key"
}

// ListenAndServe starts serving and blocks until the context is cancelled or
// the server stops.
func (s *Server) ListenAndServe(ctx context.Context) error {
	scheme := "https"
	if !s.secure {
		scheme = "http"
	}

	s.logger.Info().
		Str("address", s.http.Addr).
		Str("scheme", scheme).
		Dur("read_timeout", s.cfg.Server.HTTPReadTimeout).
		Dur("write_timeout", s.cfg.Server.HTTPWriteTimeout).
		Dur("idle_timeout", s.cfg.Server.HTTPIdleTimeout).
		Msg("server listening")

	if !s.secure {
		s.logger.Warn().Msg(
			"serving plain HTTP: session cookies lose the Secure flag, HSTS is not " +
				"sent, and credentials cross the network in the clear. Intended only " +
				"for development or behind a TLS-terminating proxy.")
	}

	errs := make(chan error, 1)
	go func() {
		var err error
		if s.secure {
			err = s.http.ListenAndServeTLS(s.cfg.Server.TLSCert, s.cfg.Server.TLSKey)
		} else {
			err = s.http.ListenAndServe()
		}
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errs <- err
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		// A signal, or the caller giving up.
		return s.Shutdown()
	case <-s.shutdown:
		// The administrator asked through the API.
		s.logger.Info().Msg("shutdown requested through the API")
		return s.Shutdown()
	}
}

// beginShutdown is what the shutdown endpoint calls. It is non-blocking, so a
// second request while one is in flight changes nothing.
func (s *Server) beginShutdown() {
	select {
	case s.shutdown <- struct{}{}:
	default:
	}
}

// Shutdown stops accepting connections and waits for in-flight requests.
//
// A shutdown that exceeds the grace period is forced and logged at WARN, rather
// than hanging: an operator who asked for a restart should get one.
func (s *Server) Shutdown() error {
	grace := s.cfg.Server.ShutdownGrace
	if grace <= 0 {
		grace = 30 * time.Second
	}

	s.logger.Info().Dur("grace", grace).Msg("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	if err := s.http.Shutdown(ctx); err != nil {
		s.logger.Warn().Err(err).Msg(
			"in-flight requests did not finish within the grace period; forcing shutdown")
		return s.http.Close()
	}

	s.logger.Info().Msg("stopped")
	return nil
}
