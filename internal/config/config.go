package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/joernott/doenerstag/internal/logging"
)

// Config is the resolved configuration for one invocation.
type Config struct {
	// File is the configuration file that was read, or empty if none existed.
	File string

	Database DatabaseConfig
	Server   ServerConfig
	Session  SessionConfig
	Log      LogConfig
	Cleanup  CleanupConfig
	Install  InstallConfig
	Mail     MailConfig
}

// DatabaseConfig holds the connection settings shared by every verb.
type DatabaseConfig struct {
	Server            string
	Port              int
	Name              string
	User              string
	Password          string `log:"-"`
	SSLMode           string
	MaxConnectionPool int
}

// ServerConfig holds the settings the server verb understands.
type ServerConfig struct {
	Port               int
	BindAddress        string
	NoHTTPS            bool
	TLSCert            string
	TLSKey             string
	NoSwagger          bool
	StaticDir          string
	HTTPReadTimeout    time.Duration
	HTTPWriteTimeout   time.Duration
	HTTPIdleTimeout    time.Duration
	ShutdownGrace      time.Duration
	MaxImageSize       int64
	CORSAllowedOrigins string

	// BaseURL is the address this installation answers on, as a person types
	// it. Only mail needs it: a link in a message cannot be relative.
	BaseURL string
}

// MailConfig holds the outgoing mail settings.
//
// Host empty means the application sends nothing. That is a supported way to
// run it, not a misconfiguration: an intranet tool in a company with no
// internal relay still works, it just cannot offer a password reset by mail.
type MailConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string `log:"-"`
	From       string
	Encryption string
	Timeout    time.Duration
}

// Enabled reports whether mail can be sent at all.
func (m MailConfig) Enabled() bool { return m.Host != "" }

// SessionConfig holds the session and login settings.
type SessionConfig struct {
	JWTSecret            string `log:"-"`
	IdleTimeout          time.Duration
	AbsoluteTimeout      time.Duration
	LoginRateLimitUser   int
	LoginRateLimitIP     int
	LoginRateLimitWindow time.Duration
}

// LogConfig holds the logging settings.
type LogConfig struct {
	Level logging.Level
	File  string
}

// CleanupConfig holds the cleanup verb's settings.
type CleanupConfig struct {
	Retention time.Duration
	DryRun    bool
}

// InstallConfig holds the install and update settings. None of these is written
// to the generated configuration file.
type InstallConfig struct {
	Output        string
	RootUser      string
	RootPassword  string `log:"-"`
	AdminUser     string
	AdminPassword string `log:"-"`

	// RootAccountPassword is the doenerstag root administrator password, not a
	// database one. It has no configuration key: like the privileged database
	// passwords, it is used once and never stored.
	RootAccountPassword string `log:"-"`

	ImprintFile    string
	LegalNotesFile string
	NonInteractive bool
}

// LoadOptions controls Load. Tests use Getenv and Scope; the application passes
// the flag set and the scope of the verb being run.
type LoadOptions struct {
	// Flags is the fully parsed flag set of the command being run.
	Flags *pflag.FlagSet

	// Scope is the scope of the verb, so that only its settings are read.
	Scope Scope

	// Getenv overrides the environment lookup. Tests set it; the application
	// leaves it nil, which means os.LookupEnv.
	Getenv func(string) (string, bool)
}

// Load resolves the configuration from all four sources, in the documented
// order of increasing precedence: built-in defaults, the configuration file,
// DOENER_* environment variables, and finally command line flags.
//
// It also enforces the two rules that must hold before anything else happens:
// no secret may be passed on the command line, and the configuration file must
// not be readable by anyone but its owner.
func Load(opts LoadOptions) (*Config, error) {
	if opts.Flags == nil {
		return nil, errors.New("config: Load requires a flag set")
	}
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.LookupEnv
	}

	// A secret on the command line is fatal, and is checked before the value is
	// read anywhere, so that it can never reach a log line or a config file.
	if err := CheckCommandLineSecrets(opts.Flags); err != nil {
		return nil, err
	}

	path, explicit := configFilePath(opts.Flags)

	v := viper.New()

	// 1. Defaults.
	for _, setting := range Settings {
		if setting.InConfigFile() {
			v.SetDefault(setting.Key, setting.Default)
		}
	}

	// 2. The configuration file.
	usedFile, err := readConfigFile(v, path, explicit)
	if err != nil {
		return nil, err
	}

	// 3. Environment variables. Bound explicitly rather than through
	//    AutomaticEnv, because the variable name derives from the flag name and
	//    not from the configuration key: --port is DOENER_PORT, not
	//    DOENER_SERVER_PORT.
	bindEnvironment(v, getenv)

	// 4. Flags. viper consults a bound flag only when it was actually changed,
	//    so an unset flag falls through to the environment, then the file, then
	//    the default. That is exactly the documented precedence.
	bindFlags(v, opts.Flags, opts.Scope)

	cfg := &Config{File: usedFile}
	if err := populate(cfg, v, opts.Flags, opts.Scope, getenv); err != nil {
		return nil, err
	}
	return cfg, nil
}

// configFilePath returns the configuration file path and whether the operator
// named it explicitly. An explicitly named file that does not exist is an
// error; a missing default file is not.
func configFilePath(flags *pflag.FlagSet) (path string, explicit bool) {
	flag := flags.Lookup(ConfigFlag)
	if flag == nil {
		return DefaultConfigFile, false
	}
	return flag.Value.String(), flag.Changed
}

func readConfigFile(v *viper.Viper, path string, explicit bool) (string, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if explicit {
			return "", fmt.Errorf("configuration file %s does not exist", path)
		}
		// No file, no problem: defaults, environment and flags still apply.
		return "", nil
	case err != nil:
		return "", fmt.Errorf("reading configuration file %s: %w", path, err)
	case info.IsDir():
		return "", fmt.Errorf("configuration file %s is a directory", path)
	}

	// The file exists, so its permissions must be acceptable before it is read.
	if err := CheckFilePermissions(path, info); err != nil {
		return "", err
	}

	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return "", fmt.Errorf("parsing configuration file %s: %w", path, err)
	}
	return path, nil
}

func bindEnvironment(v *viper.Viper, getenv func(string) (string, bool)) {
	for _, setting := range Settings {
		if !setting.InConfigFile() {
			continue
		}
		if value, ok := getenv(setting.Env()); ok {
			// Set rather than BindEnv, so that an injected Getenv works in
			// tests. Precedence is preserved because flags are bound after
			// this and viper prefers a changed flag over an explicit Set.
			v.Set(setting.Key, value)
		}
	}
}

func bindFlags(v *viper.Viper, flags *pflag.FlagSet, scope Scope) {
	for _, setting := range Settings {
		if !setting.InConfigFile() {
			continue
		}
		if !setting.Scopes.Has(scope) && !setting.Scopes.Has(ScopeGlobal) {
			continue
		}
		flag := flags.Lookup(setting.Flag)
		if flag == nil || !flag.Changed {
			continue
		}
		// An explicitly changed flag wins over everything, including the
		// environment values Set above.
		v.Set(setting.Key, flag.Value.String())
	}
}

// populate converts the resolved values into the typed Config.
func populate(cfg *Config, v *viper.Viper, flags *pflag.FlagSet, scope Scope,
	getenv func(string) (string, bool)) error {
	var errs []error

	str := func(flag string) string {
		s, _ := SettingByFlag(flag)
		return v.GetString(s.Key)
	}
	num := func(flag string) int {
		s, _ := SettingByFlag(flag)
		return v.GetInt(s.Key)
	}
	boolean := func(flag string) bool {
		s, _ := SettingByFlag(flag)
		return v.GetBool(s.Key)
	}
	dur := func(flag string) time.Duration {
		s, _ := SettingByFlag(flag)
		raw := v.GetString(s.Key)
		d, err := ParseDuration(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("--%s: %w", flag, err))
			return 0
		}
		return d
	}
	size := func(flag string) int64 {
		s, _ := SettingByFlag(flag)
		raw := v.GetString(s.Key)
		n, err := ParseByteSize(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("--%s: %w", flag, err))
			return 0
		}
		return n
	}

	cfg.Database = DatabaseConfig{
		Server:            str("database-server"),
		Port:              num("database-port"),
		Name:              str("database-name"),
		User:              str("database-user"),
		Password:          str("database-password"),
		SSLMode:           str("database-sslmode"),
		MaxConnectionPool: num("max-connection-pool"),
	}

	level, err := logging.ParseLevel(str("log-level"))
	if err != nil {
		errs = append(errs, fmt.Errorf("--log-level: %w", err))
		level = logging.DefaultLevel
	}
	cfg.Log = LogConfig{Level: level, File: str("log-file")}

	// Every section is populated, whatever the verb. Scope decides which flags
	// exist, not which settings were resolved: the file and the environment are
	// read the same way regardless, and a verb that cannot see a section is a
	// verb that destroys it.
	//
	// update is what made this concrete. Running under the install scope it
	// never populated Server or Session, so the file it rewrote carried the
	// declared defaults for both: an operator's port and timeouts silently
	// reverted, and session.jwt_secret was written empty, leaving a
	// configuration the server refuses to start from. install's own "keep the
	// existing secret so a re-run does not log everyone out" branch was
	// unreachable for exactly the same reason.
	cfg.Server = ServerConfig{
		Port:               num("port"),
		BindAddress:        str("bind-address"),
		NoHTTPS:            boolean("no-https"),
		TLSCert:            str("tls-cert"),
		TLSKey:             str("tls-key"),
		NoSwagger:          boolean("no-swagger"),
		StaticDir:          str("static-dir"),
		HTTPReadTimeout:    dur("http-read-timeout"),
		HTTPWriteTimeout:   dur("http-write-timeout"),
		HTTPIdleTimeout:    dur("http-idle-timeout"),
		ShutdownGrace:      dur("shutdown-grace"),
		MaxImageSize:       size("max-image-size"),
		CORSAllowedOrigins: str("cors-allowed-origins"),
		BaseURL:            str("base-url"),
	}
	cfg.Mail = MailConfig{
		Host:       str("mail-host"),
		Port:       num("mail-port"),
		Username:   str("mail-username"),
		Password:   str("mail-password"),
		From:       str("mail-from"),
		Encryption: str("mail-encryption"),
		Timeout:    dur("mail-timeout"),
	}
	cfg.Session = SessionConfig{
		JWTSecret:            str("jwt-secret"),
		IdleTimeout:          dur("idle-timeout"),
		AbsoluteTimeout:      dur("absolute-timeout"),
		LoginRateLimitUser:   num("login-rate-limit-user"),
		LoginRateLimitIP:     num("login-rate-limit-ip"),
		LoginRateLimitWindow: dur("login-rate-limit-window"),
	}

	cfg.Cleanup = CleanupConfig{
		Retention: dur("retention"),
		// The only flag-only value here, and absent unless cleanup registered
		// it; flagBool answers false for a flag that does not exist.
		DryRun: flagBool(flags, "dry-run"),
	}

	// cfg.Install stays behind its scope. Nothing in it is written to the
	// configuration file, and it reads privileged credentials out of the
	// environment -- which a verb that has no use for them should not do.
	if scope.Has(ScopeInstall) {
		cfg.Install = installConfig(flags, getenv)
	}

	return errors.Join(errs...)
}

// installConfig reads the install-only settings. They are never written to the
// configuration file, so they come from flags and the environment alone rather
// than from viper.
func installConfig(flags *pflag.FlagSet, getenv func(string) (string, bool)) InstallConfig {
	value := func(flag string) string {
		if v := flagString(flags, flag); v != "" {
			return v
		}
		if setting, ok := SettingByFlag(flag); ok {
			if v, present := getenv(setting.Env()); present {
				return v
			}
		}
		return ""
	}

	return InstallConfig{
		Output:              value("output"),
		RootUser:            value("database-root-user"),
		RootPassword:        value("database-root-password"),
		AdminUser:           value("database-admin-user"),
		AdminPassword:       value("database-admin-password"),
		RootAccountPassword: value("root-password"),
		ImprintFile:         value("imprint-file"),
		LegalNotesFile:      value("legal-notes-file"),
		NonInteractive:      flagBool(flags, "non-interactive"),
	}
}

func flagString(flags *pflag.FlagSet, name string) string {
	if flag := flags.Lookup(name); flag != nil {
		return flag.Value.String()
	}
	return ""
}

func flagBool(flags *pflag.FlagSet, name string) bool {
	if flag := flags.Lookup(name); flag != nil {
		return flag.Value.String() == "true"
	}
	return false
}

// PageFile returns the snippet path supplied for a content page, if any.
//
// It exists so the installer can offer the previous answer back without
// knowing which struct field belongs to which page key.
func (c InstallConfig) PageFile(key string) string {
	switch key {
	case "imprint":
		return c.ImprintFile
	case "legal_notes":
		return c.LegalNotesFile
	default:
		return ""
	}
}
