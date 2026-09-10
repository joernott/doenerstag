package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GeneratedHeader opens every configuration file this package writes.
const GeneratedHeader = "doenerstag configuration"

// sectionOrder is the order the generated file lists its sections in. A setting
// whose key has a prefix not listed here would be dropped, so the writer checks
// for that rather than silently losing it.
var sectionOrder = []string{"database", "server", "session", "mail", "log", "cleanup"}

// Values supplies the value for each setting the file records.
//
// It is keyed by flag name rather than by configuration key, because the flag
// name is what the rest of this package identifies a setting by.
type Values map[string]string

// ValuesFrom renders a resolved configuration back into writable values.
//
// Durations and byte sizes go back out in the form they came in — 604800000000000
// nanoseconds is written as "7d" — so that a generated file reads like one a
// person would write.
func ValuesFrom(cfg *Config) Values {
	v := Values{
		"database-server":      cfg.Database.Server,
		"database-port":        strconv.Itoa(cfg.Database.Port),
		"database-name":        cfg.Database.Name,
		"database-user":        cfg.Database.User,
		"database-password":    cfg.Database.Password,
		"database-sslmode":     cfg.Database.SSLMode,
		"max-connection-pool":  strconv.Itoa(cfg.Database.MaxConnectionPool),
		"log-level":            cfg.Log.Level.String(),
		"log-file":             cfg.Log.File,
		"port":                 strconv.Itoa(cfg.Server.Port),
		"bind-address":         cfg.Server.BindAddress,
		"no-https":             strconv.FormatBool(cfg.Server.NoHTTPS),
		"behind-tls-proxy":     strconv.FormatBool(cfg.Server.BehindTLSProxy),
		"tls-cert":             cfg.Server.TLSCert,
		"tls-key":              cfg.Server.TLSKey,
		"no-swagger":           strconv.FormatBool(cfg.Server.NoSwagger),
		"static-dir":           cfg.Server.StaticDir,
		"http-read-timeout":    FormatDuration(cfg.Server.HTTPReadTimeout),
		"http-write-timeout":   FormatDuration(cfg.Server.HTTPWriteTimeout),
		"http-idle-timeout":    FormatDuration(cfg.Server.HTTPIdleTimeout),
		"shutdown-grace":       FormatDuration(cfg.Server.ShutdownGrace),
		"max-image-size":       FormatByteSize(cfg.Server.MaxImageSize),
		"cors-allowed-origins": cfg.Server.CORSAllowedOrigins,
		"base-url":             cfg.Server.BaseURL,

		"mail-host":       cfg.Mail.Host,
		"mail-port":       strconv.Itoa(cfg.Mail.Port),
		"mail-username":   cfg.Mail.Username,
		"mail-password":   cfg.Mail.Password,
		"mail-from":       cfg.Mail.From,
		"mail-encryption": cfg.Mail.Encryption,
		"mail-timeout":    FormatDuration(cfg.Mail.Timeout),

		"jwt-secret":              cfg.Session.JWTSecret,
		"idle-timeout":            FormatDuration(cfg.Session.IdleTimeout),
		"absolute-timeout":        FormatDuration(cfg.Session.AbsoluteTimeout),
		"login-rate-limit-user":   strconv.Itoa(cfg.Session.LoginRateLimitUser),
		"login-rate-limit-ip":     strconv.Itoa(cfg.Session.LoginRateLimitIP),
		"login-rate-limit-window": FormatDuration(cfg.Session.LoginRateLimitWindow),

		"retention": FormatDuration(cfg.Cleanup.Retention),
	}

	// A zero value in the struct means the section was never populated, because
	// the verb had no use for it. Fall back to the declared default so the
	// generated file still documents every setting.
	for _, setting := range Settings {
		if !setting.InConfigFile() {
			continue
		}
		if current, ok := v[setting.Flag]; !ok || isZeroValue(setting, current) {
			v[setting.Flag] = defaultString(setting)
		}
	}
	return v
}

// isZeroValue reports whether a rendered value is the type's zero rather than a
// deliberate setting. An unset value falls back to the declared default, which
// is what makes the generated file document every setting.
//
// An empty string counts as unset. That is safe because every setting whose
// default is non-empty -- the TLS paths, the static directory, the database
// coordinates -- has no meaning when empty, and for the rest the default is
// empty too, so the fallback changes nothing. Before this, install wrote
// tls_cert: "" over the declared server.crt and the server it had just
// installed refused to start with "no TLS certificate was configured".
func isZeroValue(setting Setting, rendered string) bool {
	switch setting.Kind {
	case KindInt:
		return rendered == "0"
	case KindDuration:
		return rendered == "0s" || rendered == ""
	case KindByteSize:
		return rendered == "0" || rendered == ""
	default:
		return rendered == ""
	}
}

func defaultString(setting Setting) string {
	switch value := setting.Default.(type) {
	case string:
		return value
	case int:
		return strconv.Itoa(value)
	case bool:
		return strconv.FormatBool(value)
	default:
		return ""
	}
}

// Render produces the configuration file body.
//
// Every setting appears, with the comment that explains it, so the file doubles
// as documentation. A setting the operator never touched is written at its
// default rather than omitted: an operator reading the file should not have to
// know what is missing.
func Render(values Values, generatedAt time.Time) (string, error) {
	return RenderFile(values, RenderOptions{GeneratedAt: generatedAt, Verb: "install"})
}

// RenderFile renders the configuration file, including anything the previous
// file had that this binary no longer knows about.
func RenderFile(values Values, opts RenderOptions) (string, error) {
	if err := checkSectionsAreKnown(); err != nil {
		return "", err
	}

	verb := opts.Verb
	if verb == "" {
		verb = "install"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n", GeneratedHeader)
	fmt.Fprintf(&b, "# Generated by doenerstag %s on %s\n",
		verb, opts.GeneratedAt.UTC().Format(time.RFC3339))
	b.WriteString("#\n")
	b.WriteString("# Every setting is listed with its explanatory comment, so this file is also\n")
	b.WriteString("# the reference. Any of them can be overridden by a DOENER_* environment\n")
	b.WriteString("# variable or, except for the secrets, by a command line flag.\n")
	b.WriteString("#\n")
	b.WriteString("# This file holds the database password and the session signing secret, so it\n")
	b.WriteString("# must stay readable only by its owner. The application refuses to start\n")
	b.WriteString("# otherwise.\n")

	for _, section := range sectionOrder {
		settings := settingsInSection(section)
		if len(settings) == 0 {
			continue
		}

		fmt.Fprintf(&b, "\n%s:\n", section)
		for _, setting := range settings {
			writeSetting(&b, setting, values[setting.Flag])
		}
	}

	renderRetired(&b, opts.Retired)
	return b.String(), nil
}

func writeSetting(b *strings.Builder, setting Setting, value string) {
	// An untouched setting is written exactly as the specification declares it,
	// so the generated file reads like the documentation it is meant to be.
	// Without this, a 60s default would come back as "1m" — the same duration,
	// but no longer the string the reference says the default is.
	if equalsDefault(setting, value) {
		value = defaultString(setting)
	}

	fmt.Fprintf(b, "  # %s.\n", capitalise(setting.Usage))

	if setting.Secret {
		fmt.Fprintf(b, "  # Never accepted on the command line. May also be supplied as %s.\n",
			setting.Env())
	} else {
		fmt.Fprintf(b, "  # Flag: --%s. Environment: %s. Default: %s\n",
			setting.Flag, setting.Env(), renderDefault(setting))
	}

	leaf := leafKey(setting.Key)
	fmt.Fprintf(b, "  %s: %s\n", leaf, yamlScalar(setting, value))
}

// equalsDefault reports whether a value means the same as the declared default.
//
// Durations and byte sizes are compared by what they mean rather than by their
// text, so "60s" and "1m" count as unchanged, which is the whole point.
func equalsDefault(setting Setting, value string) bool {
	declared := defaultString(setting)
	if value == declared {
		return true
	}

	switch setting.Kind {
	case KindDuration:
		got, gotErr := ParseDuration(value)
		want, wantErr := ParseDuration(declared)
		return gotErr == nil && wantErr == nil && got == want
	case KindByteSize:
		got, gotErr := ParseByteSize(value)
		want, wantErr := ParseByteSize(declared)
		return gotErr == nil && wantErr == nil && got == want
	default:
		return false
	}
}

func renderDefault(setting Setting) string {
	value := defaultString(setting)
	if value == "" {
		return "(empty)"
	}
	return value
}

// yamlScalar renders a value as a YAML scalar.
//
// Strings are always double-quoted, so a password containing a colon, a hash or
// a leading zero survives the round trip instead of becoming a map, a comment
// or a number.
func yamlScalar(setting Setting, value string) string {
	switch setting.Kind {
	case KindInt, KindBool:
		if value == "" {
			return defaultString(setting)
		}
		return value
	default:
		return quote(value)
	}
}

func quote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] -= 'a' - 'A'
	}
	return string(runes)
}

func settingsInSection(section string) []Setting {
	var out []Setting
	for _, setting := range Settings {
		if setting.InConfigFile() && sectionOf(setting.Key) == section {
			out = append(out, setting)
		}
	}
	return out
}

func sectionOf(key string) string {
	section, _, found := strings.Cut(key, ".")
	if !found {
		return ""
	}
	return section
}

func leafKey(key string) string {
	_, leaf, found := strings.Cut(key, ".")
	if !found {
		return key
	}
	return leaf
}

// checkSectionsAreKnown fails when a setting's key uses a section the writer
// does not know about, which would otherwise drop it from the generated file
// without a word.
func checkSectionsAreKnown() error {
	known := make(map[string]bool, len(sectionOrder))
	for _, section := range sectionOrder {
		known[section] = true
	}

	var unknown []string
	for _, setting := range Settings {
		if !setting.InConfigFile() {
			continue
		}
		if section := sectionOf(setting.Key); !known[section] {
			unknown = append(unknown, setting.Key)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"these settings use a configuration section the file writer does not know, "+
				"so they would be silently dropped: %s", strings.Join(unknown, ", "))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Writing the file
// ---------------------------------------------------------------------------

// CheckWritable reports whether a configuration file could be written at path,
// without creating, truncating or otherwise touching an existing file there.
//
// The installer calls this before it asks the operator a single question. Half
// an hour of answers followed by "permission denied" is a bad trade, and the
// obvious cheap test — opening the destination for writing — would truncate the
// very file whose values are being offered as defaults.
//
// What it actually tests is the directory, because that is what an atomic
// rename needs.
func CheckWritable(path string) error {
	dir := filepath.Dir(path)

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cannot write %s: %s is not a directory", path, dir)
	}

	probe, err := os.CreateTemp(dir, ".doenerstag-writable-*")
	if err != nil {
		return fmt.Errorf("cannot write in %s: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("cannot clean up the write test in %s: %w", dir, err)
	}

	// An existing destination must also be replaceable.
	if existing, err := os.Stat(path); err == nil {
		if existing.IsDir() {
			return fmt.Errorf("cannot write %s: it is a directory", path)
		}
		// The path is the operator's own --config or --output value: opening it
		// is the entire purpose of this function, and there is nothing to
		// sanitise it against. Deliberately no O_TRUNC and no O_CREATE, so the
		// probe cannot damage the file it is asking about.
		file, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // operator-supplied path, opened read-write-none
		if err != nil {
			return fmt.Errorf("cannot write %s: %w", path, err)
		}
		_ = file.Close()
	}

	return nil
}

// WriteFile writes the configuration atomically, with mode 0600.
//
// The content goes to a temporary file in the same directory and is renamed
// into place only once it is complete and flushed. Writing in place would leave
// a truncated file if anything failed part way — and when the output path is
// the input path, which is the default, that file is the one being read for its
// own defaults.
func WriteFile(path, content string) error {
	dir := filepath.Dir(path)

	temp, err := os.CreateTemp(dir, ".doenerstag-config-*")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w", dir, err)
	}
	tempName := temp.Name()

	// Remove the temporary file on any path that does not reach the rename.
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()

	// Set the mode before writing, so the secrets are never briefly readable.
	if err := os.Chmod(tempName, ConfigFileMode); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", tempName, err)
	}

	if _, err := temp.WriteString(content); err != nil {
		return fmt.Errorf("writing %s: %w", tempName, err)
	}
	// Flush to disk before the rename, so a crash cannot leave the new name
	// pointing at an empty file.
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("flushing %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tempName, err)
	}

	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	// Rename preserves the temporary file's mode, but an existing destination
	// on some systems keeps its own; set it explicitly rather than assume.
	if err := os.Chmod(path, ConfigFileMode); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", path, err)
	}
	return nil
}
