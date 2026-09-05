package logging

import (
	"context"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/rs/zerolog"
)

// Standard field names. Every line carries the first four; request-scoped lines
// add the rest. See docs/08_technologies.md.
const (
	FieldTime       = "time"
	FieldLevel      = "level"
	FieldMessage    = "message"
	FieldComponent  = "component"
	FieldRequestID  = "request_id"
	FieldUser       = "user"
	FieldMethod     = "method"
	FieldPath       = "path"
	FieldStatus     = "status"
	FieldDurationMs = "duration_ms"
	FieldError      = "error"
)

// AnonymousUser is the value of the user field when no one is authenticated.
const AnonymousUser = "-"

// timeFormat is RFC 3339 with milliseconds. Timestamps are always UTC.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// Options configures a Logger.
type Options struct {
	// Level is the least severe level that is emitted.
	Level Level

	// File is the path to write to. Empty means stdout.
	File string

	// Output overrides the destination entirely, ignoring File. Tests use it;
	// the application does not.
	Output io.Writer
}

// reopener is the part of a destination that survives log rotation.
type reopener interface {
	io.Writer
	Reopen() error
	Close() error
}

// Logger owns the configured zerolog logger and its destination.
type Logger struct {
	zl   zerolog.Logger
	dest reopener
}

// New builds a Logger from opts. The caller owns it and should Close it on
// shutdown.
func New(opts Options) (*Logger, error) {
	level := opts.Level
	if !level.Valid() {
		level = DefaultLevel
	}

	var dest reopener
	switch {
	case opts.Output != nil:
		dest = nopCloser{Writer: opts.Output}
	case opts.File == "":
		dest = nopCloser{Writer: os.Stdout}
	default:
		w, err := newReopenWriter(opts.File)
		if err != nil {
			return nil, err
		}
		dest = w
	}

	zl := zerolog.New(dest).
		Level(level.Zerolog()).
		With().
		Timestamp().
		Logger()

	return &Logger{zl: zl, dest: dest}, nil
}

// Configure sets the zerolog package-level formatting that the whole
// application shares: UTC timestamps in RFC 3339 with milliseconds, and the
// documented field names.
//
// It is called by New, and separately by tests that build a zerolog.Logger
// directly. Calling it more than once is harmless.
func Configure() {
	zerolog.TimeFieldFormat = timeFormat
	zerolog.TimestampFieldName = FieldTime
	zerolog.LevelFieldName = FieldLevel
	zerolog.MessageFieldName = FieldMessage
	zerolog.ErrorFieldName = FieldError
	zerolog.TimestampFunc = func() time.Time { return time.Now().UTC() }
}

func init() {
	Configure()
}

// Zerolog returns the underlying logger. It is a pointer because zerolog
// declares its level methods on *Logger.
func (l *Logger) Zerolog() *zerolog.Logger { return &l.zl }

// Component returns a logger that stamps every line with a component name,
// such as "api", "db" or "auth".
func (l *Logger) Component(name string) *zerolog.Logger {
	component := l.zl.With().Str(FieldComponent, name).Logger()
	return &component
}

// Reopen closes and reopens the log file. It is a no-op when logging to stdout.
func (l *Logger) Reopen() error { return l.dest.Reopen() }

// Close releases the destination. Closing a stdout logger does nothing.
func (l *Logger) Close() error { return l.dest.Close() }

// HandleReopenSignal reopens the log file whenever the reopen signal arrives,
// until ctx is cancelled. On Unix that signal is SIGHUP, which is what the
// logrotate configuration in docs/10_operations.md sends. On Windows no such
// signal exists and this returns immediately.
//
// It runs in its own goroutine and returns at once.
func (l *Logger) HandleReopenSignal(ctx context.Context) {
	signals := reopenSignals()
	if len(signals) == 0 {
		return
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)

	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				if err := l.Reopen(); err != nil {
					l.zl.Error().Err(err).
						Str(FieldComponent, "logging").
						Msg("could not reopen log file after rotation signal")
					continue
				}
				l.zl.Info().
					Str(FieldComponent, "logging").
					Msg("reopened log file after rotation signal")
			}
		}
	}()
}

// FuncCall emits the DEBUG-level function entry line described in
// docs/08_technologies.md: the function name and its parameters, with every
// sensitive parameter redacted.
//
// It is a no-op unless DEBUG is enabled, so building the argument map is the
// only cost at other levels. Callers should not do expensive work to construct
// that map.
func FuncCall(zl *zerolog.Logger, function string, args map[string]any) {
	event := zl.Debug()
	if !event.Enabled() {
		return
	}
	event = event.Str("func", function)
	event = AddFields(event, args)
	event.Msg("call")
}
