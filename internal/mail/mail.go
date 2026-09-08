// Package mail sends the few messages this application has to send.
//
// It is deliberately small. doenerstag sends one kind of message -- a password
// reset link -- and a general-purpose mail library would be several thousand
// lines of dependency to compose a message with one recipient, one subject and
// a plain text body. net/smtp is in the standard library and does exactly that.
//
// What this package does add, because net/smtp does not, is the operational
// behaviour docs/10_operations.md promises: a timeout, so a mail server that
// accepts connections and then says nothing cannot hold a request open; header
// injection refused rather than escaped; and an error that names the mail
// server rather than the caller.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Encryption is how the connection to the mail server is protected.
type Encryption string

// The three answers. There are three rather than two because implicit TLS on
// port 465 is not "more STARTTLS": the handshake happens before any SMTP is
// spoken, so the client has to decide before it connects.
const (
	EncryptionNone     Encryption = "none"
	EncryptionSTARTTLS Encryption = "starttls"
	EncryptionTLS      Encryption = "tls"
)

// ParseEncryption reads the configured value.
func ParseEncryption(value string) (Encryption, error) {
	switch Encryption(strings.ToLower(strings.TrimSpace(value))) {
	case EncryptionNone:
		return EncryptionNone, nil
	case EncryptionSTARTTLS, "":
		return EncryptionSTARTTLS, nil
	case EncryptionTLS:
		return EncryptionTLS, nil
	default:
		return "", fmt.Errorf("mail encryption %q is not one of none, starttls or tls", value)
	}
}

// Options describe the mail server and the sender.
type Options struct {
	Host     string
	Port     int
	Username string
	Password string

	// From is the address messages are sent as. Empty means
	// doenerstag@<hostname>, which is what a machine with no configured
	// identity can honestly claim to be.
	From string

	Encryption Encryption

	// Timeout bounds the whole exchange: connect, handshake, and the
	// conversation. A mail server that accepts a connection and then stops
	// talking is a common enough failure that leaving this unbounded means a
	// request thread held until the HTTP write timeout kills it.
	Timeout time.Duration

	// InsecureSkipVerify disables certificate verification. Intended for a
	// development mail server with a self-signed certificate and nothing else;
	// there is no configuration setting for it.
	InsecureSkipVerify bool
}

// Message is one mail.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender sends messages.
//
// An interface because the tests for everything above this -- the password
// reset flow especially -- need to see what would have been sent without
// standing up a mail server, and because an installation with no mail server
// configured substitutes one that refuses politely.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// ErrNotConfigured is what a Sender returns when no mail server is configured.
//
// A distinct error rather than a generic failure: the API turns it into
// something a person can act on ("this installation cannot send mail; ask an
// administrator") rather than into "something went wrong".
var ErrNotConfigured = errors.New("no mail server is configured")

// Disabled is the Sender an installation with no mail-host gets.
type Disabled struct{}

// Send always fails, with the error that says why.
func (Disabled) Send(context.Context, Message) error { return ErrNotConfigured }

// SMTP sends over SMTP.
type SMTP struct {
	opts Options
}

// New returns a Sender for the options given, or Disabled when no host is set.
func New(opts Options) Sender {
	if opts.Host == "" {
		return Disabled{}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.Encryption == "" {
		opts.Encryption = EncryptionSTARTTLS
	}
	if opts.From == "" {
		opts.From = defaultFrom()
	}
	return &SMTP{opts: opts}
}

// defaultFrom is what the machine can honestly claim to be.
func defaultFrom() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	return "doenerstag@" + host
}

// From reports the address messages are sent as.
func (s *SMTP) From() string { return s.opts.From }

// Send delivers one message.
func (s *SMTP) Send(ctx context.Context, msg Message) error {
	if err := validateHeaderValue("recipient", msg.To); err != nil {
		return err
	}
	if err := validateHeaderValue("subject", msg.Subject); err != nil {
		return err
	}

	body, err := s.compose(msg)
	if err != nil {
		return err
	}

	address := net.JoinHostPort(s.opts.Host, fmt.Sprint(s.opts.Port))

	// The context bounds the whole exchange. net/smtp has no context support of
	// its own, so the deadline is applied to the connection: every read and
	// write after this point fails once it passes, which is what turns a mail
	// server that stops talking into an error rather than a hung request.
	deadline := time.Now().Add(s.opts.Timeout)
	if fromCtx, ok := ctx.Deadline(); ok && fromCtx.Before(deadline) {
		deadline = fromCtx
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connecting to the mail server at %s: %w", address, err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("setting the mail deadline: %w", err)
	}

	if s.opts.Encryption == EncryptionTLS {
		tlsConn := tls.Client(conn, s.tlsConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("TLS handshake with the mail server at %s: %w", address, err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.opts.Host)
	if err != nil {
		return fmt.Errorf("greeting the mail server at %s: %w", address, err)
	}
	defer func() { _ = client.Close() }()

	if s.opts.Encryption == EncryptionSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(s.tlsConfig()); err != nil {
				return fmt.Errorf("STARTTLS with the mail server at %s: %w", address, err)
			}
		}
		// A server that does not offer STARTTLS is not refused. On an intranet
		// relay that does not speak it, refusing would mean no mail at all,
		// and the alternative -- silently configuring "none" -- would be worse
		// because nothing would say so. The startup check logs a warning.
	}

	if s.opts.Username != "" {
		auth := smtp.PlainAuth("", s.opts.Username, s.opts.Password, s.opts.Host)
		if err := client.Auth(auth); err != nil {
			// The password is not in the error. An authentication failure is
			// logged, and a log line that carries the credential that failed is
			// a credential in the log.
			return fmt.Errorf("authenticating to the mail server at %s as %q: %w",
				address, s.opts.Username, err)
		}
	}

	if err := client.Mail(s.opts.From); err != nil {
		return fmt.Errorf("sending from %q: %w", s.opts.From, err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("the mail server refused the recipient: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("starting the message body: %w", err)
	}
	if _, err := writer.Write([]byte(body)); err != nil {
		return fmt.Errorf("writing the message body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("completing the message: %w", err)
	}

	return client.Quit()
}

func (s *SMTP) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         s.opts.Host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: s.opts.InsecureSkipVerify, //nolint:gosec // false unless a caller asked for it, and nothing in the configuration can
	}
}

// compose builds the message.
//
// Plain text, UTF-8, quoted-printable-free: the one message this sends is a
// sentence and a link. HTML mail would mean a multipart body, an alternative
// plain part, and a link that some clients rewrite -- for no gain to somebody
// who needs to click one thing.
func (s *SMTP) compose(msg Message) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "From: %s\r\n", s.opts.From)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", encodeSubject(msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("\r\n")

	// Bare newlines in a body are legal to write but not to send: the dot
	// stuffing and line endings SMTP requires are the writer's job, and
	// net/smtp's DataWriter does not do them.
	b.WriteString(strings.ReplaceAll(normaliseNewlines(msg.Body), "\n", "\r\n"))

	return b.String(), nil
}

func normaliseNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// encodeSubject encodes a subject that is not plain ASCII.
//
// Without this a German subject arrives as mojibake in half the clients that
// see it. RFC 2047, base64, which is what every client has understood for
// twenty years.
func encodeSubject(subject string) string {
	if isASCII(subject) {
		return subject
	}
	return mimeEncode(subject)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// validateHeaderValue refuses a value that would inject a header.
//
// A recipient address or a subject that contains a newline can add headers to
// the message: a Bcc, a different From, a second body. The values here come
// from a database row an administrator typed, not from an anonymous request,
// which makes this defence in depth rather than the only defence -- but the
// cost of it is four lines.
func validateHeaderValue(what, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("the %s contains a line break, which would inject a mail header", what)
	}
	if value == "" && what == "recipient" {
		return errors.New("no recipient address")
	}
	return nil
}

// mimeEncode is RFC 2047 base64 in UTF-8.
func mimeEncode(s string) string {
	return mime.BEncoding.Encode("UTF-8", s)
}
