// Package config wires cobra and viper together and handles the configuration
// file, as described in docs/09_configuration.md.
//
// Every setting is declared once in the registry below. Flag registration,
// environment binding, defaults and the generated configuration file are all
// derived from it, so a setting cannot exist in one of those places and be
// missing from another.
package config

import (
	"strings"
)

// EnvPrefix is prepended to every environment variable this application reads.
const EnvPrefix = "DOENER"

// Scope says which verbs understand a setting. A setting may belong to several.
type Scope uint8

// The scopes. ScopeInstall covers both install and update, which take the same
// flags.
const (
	ScopeGlobal Scope = 1 << iota
	ScopeServer
	ScopeInstall
	ScopeCleanup
)

// Has reports whether s includes scope.
func (s Scope) Has(scope Scope) bool { return s&scope != 0 }

// Kind is the type of a setting's value, which determines how it is parsed and
// how it is registered as a flag.
type Kind uint8

// The setting kinds.
const (
	KindString Kind = iota
	KindInt
	KindBool
	KindDuration
	KindByteSize
)

// Setting describes one configuration setting.
type Setting struct {
	// Flag is the long flag name without the leading dashes.
	Flag string

	// Short is the single-character shorthand, or empty.
	Short string

	// Key is the dotted configuration file key, or empty for settings that are
	// never written to the file.
	Key string

	Kind    Kind
	Default any
	Usage   string
	Scopes  Scope

	// Secret marks a value that may not be passed on the command line. The flag
	// is still registered, hidden, so that passing it produces the documented
	// FATAL security error rather than an unhelpful "unknown flag".
	Secret bool
}

// Env returns the environment variable for a setting: the prefix, then the flag
// name upper-cased with dashes replaced by underscores.
//
// Note that this derives from the flag name, not the configuration key, so
// --port is DOENER_PORT and not DOENER_SERVER_PORT. That is what
// docs/09_configuration.md specifies.
func (s Setting) Env() string {
	return EnvPrefix + "_" + strings.ToUpper(strings.ReplaceAll(s.Flag, "-", "_"))
}

// InConfigFile reports whether the setting is written to the generated
// configuration file.
func (s Setting) InConfigFile() bool { return s.Key != "" }

// ConfigFlag is the flag naming the configuration file itself. It is handled
// outside the registry because it selects the file the rest are read from.
const ConfigFlag = "config"

// DefaultConfigFile is used when --config is not given.
const DefaultConfigFile = "doenerstag.yaml"

// Settings is the single declaration of every configuration setting.
//
// Adding a setting here is all that is needed for it to gain a flag, an
// environment variable, a configuration file key, a default and an entry in the
// generated configuration file.
var Settings = []Setting{
	// --- global ------------------------------------------------------------
	{
		Flag: "database-server", Short: "S", Key: "database.server",
		Kind: KindString, Default: "localhost",
		Usage:  "PostgreSQL host name",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "database-port", Short: "P", Key: "database.port",
		Kind: KindInt, Default: 5432,
		Usage:  "PostgreSQL port",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "database-name", Short: "N", Key: "database.name",
		Kind: KindString, Default: "doenerstag",
		Usage:  "database name",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "database-user", Short: "U", Key: "database.user",
		Kind: KindString, Default: "doener",
		Usage:  "database user used at runtime",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "database-password", Key: "database.password",
		Kind: KindString, Default: "", Secret: true,
		Usage:  "password for the runtime database user (config file or environment only)",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "database-sslmode", Key: "database.sslmode",
		Kind: KindString, Default: "prefer",
		Usage:  "TLS mode for the database connection: disable, allow, prefer, require, verify-ca or verify-full",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "max-connection-pool", Key: "database.max_connection_pool",
		Kind: KindInt, Default: 10,
		Usage:  "upper bound for open and for idle database connections",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "log-level", Short: "l", Key: "log.level",
		Kind: KindString, Default: "INFO",
		Usage:  "log level: FATAL, ERROR, WARN, INFO or DEBUG",
		Scopes: ScopeGlobal,
	},
	{
		Flag: "log-file", Short: "f", Key: "log.file",
		Kind: KindString, Default: "",
		Usage:  "log destination; empty writes to stdout",
		Scopes: ScopeGlobal,
	},

	// --- server ------------------------------------------------------------
	{
		Flag: "port", Short: "p", Key: "server.port",
		Kind: KindInt, Default: 8443,
		Usage:  "TCP port to listen on",
		Scopes: ScopeServer,
	},
	{
		Flag: "bind-address", Short: "b", Key: "server.bind_address",
		Kind: KindString, Default: "",
		Usage:  "address to bind to; empty binds to all addresses",
		Scopes: ScopeServer,
	},
	{
		Flag: "no-https", Key: "server.no_https",
		Kind: KindBool, Default: false,
		Usage:  "serve plain HTTP instead of HTTPS",
		Scopes: ScopeServer,
	},
	{
		Flag: "tls-cert", Short: "t", Key: "server.tls_cert",
		Kind: KindString, Default: "server.crt",
		Usage:  "PEM certificate chain",
		Scopes: ScopeServer,
	},
	{
		Flag: "tls-key", Short: "T", Key: "server.tls_key",
		Kind: KindString, Default: "server.key",
		Usage:  "PEM private key",
		Scopes: ScopeServer,
	},
	{
		Flag: "no-swagger", Key: "server.no_swagger",
		Kind: KindBool, Default: false,
		Usage:  "do not serve the Swagger UI",
		Scopes: ScopeServer,
	},
	{
		Flag: "static-dir", Short: "s", Key: "server.static_dir",
		Kind: KindString, Default: "static",
		Usage:  "directory to serve assets from; ignored in embedded builds",
		Scopes: ScopeServer,
	},
	{
		Flag: "http-read-timeout", Key: "server.http_read_timeout",
		Kind: KindDuration, Default: "30s",
		Usage:  "maximum time to read a request, including its body",
		Scopes: ScopeServer,
	},
	{
		Flag: "http-write-timeout", Key: "server.http_write_timeout",
		Kind: KindDuration, Default: "60s",
		Usage:  "maximum time to write a response; the event stream is exempt",
		Scopes: ScopeServer,
	},
	{
		Flag: "http-idle-timeout", Key: "server.http_idle_timeout",
		Kind: KindDuration, Default: "120s",
		Usage:  "maximum time a keep-alive connection may sit idle",
		Scopes: ScopeServer,
	},
	{
		Flag: "shutdown-grace", Key: "server.shutdown_grace",
		Kind: KindDuration, Default: "30s",
		Usage:  "how long to wait for in-flight requests during shutdown",
		Scopes: ScopeServer,
	},
	{
		Flag: "max-image-size", Key: "server.max_image_size",
		Kind: KindByteSize, Default: "5MiB",
		Usage:  "largest accepted image upload, for example 5MiB",
		Scopes: ScopeServer,
	},
	{
		Flag: "cors-allowed-origins", Key: "server.cors_allowed_origins",
		Kind: KindString, Default: "",
		Usage:  "comma-separated origins allowed to call the API cross-origin; empty sends no CORS headers",
		Scopes: ScopeServer,
	},
	{
		Flag: "jwt-secret", Key: "session.jwt_secret",
		Kind: KindString, Default: "", Secret: true,
		Usage:  "signing key for session tokens (config file or environment only)",
		Scopes: ScopeServer,
	},
	{
		Flag: "idle-timeout", Key: "session.idle_timeout",
		Kind: KindDuration, Default: "6h",
		Usage:  "session idle timeout",
		Scopes: ScopeServer,
	},
	{
		Flag: "absolute-timeout", Key: "session.absolute_timeout",
		Kind: KindDuration, Default: "7d",
		Usage:  "session lifetime regardless of activity",
		Scopes: ScopeServer,
	},
	{
		Flag: "login-rate-limit-user", Key: "session.login_rate_limit_user",
		Kind: KindInt, Default: 10,
		Usage:  "failed logins per user name per window",
		Scopes: ScopeServer,
	},
	{
		Flag: "login-rate-limit-ip", Key: "session.login_rate_limit_ip",
		Kind: KindInt, Default: 60,
		Usage:  "failed logins per client address per window",
		Scopes: ScopeServer,
	},
	{
		Flag: "login-rate-limit-window", Key: "session.login_rate_limit_window",
		Kind: KindDuration, Default: "15m",
		Usage:  "rate limit window",
		Scopes: ScopeServer,
	},

	// --- outgoing mail -----------------------------------------------------
	//
	// Mail is optional. With no host configured the application sends nothing
	// and says so where it matters, rather than failing at the moment somebody
	// asks for a password reset. An intranet tool in a company that has no
	// internal mail relay is a reasonable thing to run.
	{
		Flag: "mail-host", Key: "mail.host",
		Kind: KindString, Default: "",
		Usage:  "SMTP host for outgoing mail; empty disables sending",
		Scopes: ScopeServer,
	},
	{
		Flag: "mail-port", Key: "mail.port",
		Kind: KindInt, Default: 25,
		Usage:  "SMTP port",
		Scopes: ScopeServer,
	},
	{
		Flag: "mail-username", Key: "mail.username",
		Kind: KindString, Default: "",
		Usage:  "user name for SMTP authentication; empty sends unauthenticated",
		Scopes: ScopeServer,
	},
	{
		Flag: "mail-password", Key: "mail.password",
		Kind: KindString, Default: "", Secret: true,
		Usage:  "password for SMTP authentication (config file or environment only)",
		Scopes: ScopeServer,
	},
	{
		Flag: "mail-from", Key: "mail.from",
		Kind: KindString, Default: "",
		Usage:  "envelope and header From address; defaults to doenerstag@<hostname>",
		Scopes: ScopeServer,
	},
	{
		// none, starttls or tls. Not a bool, because there are three answers
		// and the third -- implicit TLS on port 465 -- is not "more" or "less"
		// of the second.
		Flag: "mail-encryption", Key: "mail.encryption",
		Kind: KindString, Default: "starttls",
		Usage:  "transport security for SMTP: none, starttls or tls",
		Scopes: ScopeServer,
	},
	{
		Flag: "mail-timeout", Key: "mail.timeout",
		Kind: KindDuration, Default: "10s",
		Usage:  "how long to wait for the mail server before giving up",
		Scopes: ScopeServer,
	},
	{
		// Sending a message with a link in it means knowing the address this
		// installation answers on, which nothing else in the configuration
		// says: --port and --bind-address describe the socket, not the name a
		// person types, and a reverse proxy makes the two different.
		Flag: "base-url", Key: "server.base_url",
		Kind: KindString, Default: "",
		Usage:  "public address of this installation, for links in mail, e.g. https://doener.example",
		Scopes: ScopeGlobal,
	},

	// --- install and update ------------------------------------------------
	// These are never written to the configuration file, so they have no Key.
	{
		Flag: "output", Short: "o",
		Kind: KindString, Default: "",
		Usage:  "configuration file to write; defaults to the value of --config",
		Scopes: ScopeInstall,
	},
	{
		Flag: "database-root-user", Short: "R",
		Kind: KindString, Default: "",
		Usage:  "database user permitted to create databases and roles; never stored",
		Scopes: ScopeInstall,
	},
	{
		Flag: "database-root-password",
		Kind: KindString, Default: "", Secret: true,
		Usage:  "password for the root database user (environment or prompt only)",
		Scopes: ScopeInstall,
	},
	{
		Flag: "database-admin-user", Short: "A",
		Kind: KindString, Default: "",
		Usage:  "database user owning the schema; never stored",
		Scopes: ScopeInstall,
	},
	{
		Flag: "database-admin-password",
		Kind: KindString, Default: "", Secret: true,
		Usage:  "password for the admin database user (environment or prompt only)",
		Scopes: ScopeInstall,
	},
	{
		// The doenerstag root account, not a database role. The database
		// identities all carry a "database-" prefix; this one is the
		// application's own administrator.
		Flag: "root-password",
		Kind: KindString, Default: "", Secret: true,
		Usage:  "password for the doenerstag root administrator (environment or prompt only)",
		Scopes: ScopeInstall,
	},
	{
		Flag: "imprint-file",
		Kind: KindString, Default: "",
		Usage:  "HTML snippet loaded into the imprint page",
		Scopes: ScopeInstall,
	},
	{
		Flag: "legal-notes-file",
		Kind: KindString, Default: "",
		Usage:  "HTML snippet loaded into the legal notes page",
		Scopes: ScopeInstall,
	},
	{
		Flag: "non-interactive",
		Kind: KindBool, Default: false,
		Usage:  "ask nothing; fail if a required value is missing",
		Scopes: ScopeInstall,
	},

	// --- cleanup -----------------------------------------------------------
	{
		Flag: "retention", Short: "r", Key: "cleanup.retention",
		Kind: KindDuration, Default: "14d",
		Usage:  "orders whose deadline is older than this are deleted",
		Scopes: ScopeCleanup,
	},
	{
		Flag: "dry-run",
		Kind: KindBool, Default: false,
		Usage:  "report what would be deleted and change nothing",
		Scopes: ScopeCleanup,
	},
}

// SettingByFlag returns the setting with the given flag name.
func SettingByFlag(flag string) (Setting, bool) {
	for _, s := range Settings {
		if s.Flag == flag {
			return s, true
		}
	}
	return Setting{}, false
}

// SecretSettings returns the settings that may not be passed on the command
// line.
func SecretSettings() []Setting {
	var secrets []Setting
	for _, s := range Settings {
		if s.Secret {
			secrets = append(secrets, s)
		}
	}
	return secrets
}

// SettingsInScope returns the settings a verb with this scope understands.
func SettingsInScope(scope Scope) []Setting {
	var out []Setting
	for _, s := range Settings {
		if s.Scopes.Has(scope) {
			out = append(out, s)
		}
	}
	return out
}
