package mail_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/mail"
)

// fakeSMTP is enough of a mail server to accept one message and remember it.
//
// A real mail server in a container would test more, and the mokapi instance in
// contrib/setup_dev_pipeline.sh is there for exactly that. This is what the unit
// tests use, because a test that needs Docker is a test that does not run on a
// developer's laptop during `make test`.
type fakeSMTP struct {
	address string

	mu       sync.Mutex
	received []string
	// refuse is the SMTP verb to answer with a permanent failure, or empty.
	refuse string
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	f := &fakeSMTP{address: listener.Addr().String()}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) host() string {
	host, _, _ := net.SplitHostPort(f.address)
	return host
}

func (f *fakeSMTP) port() int {
	_, port, _ := net.SplitHostPort(f.address)
	n := 0
	for _, r := range port {
		n = n*10 + int(r-'0')
	}
	return n
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

	write("220 fake ESMTP")

	var body strings.Builder
	inData := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false
				f.mu.Lock()
				f.received = append(f.received, body.String())
				f.mu.Unlock()
				body.Reset()
				write("250 stored")
				continue
			}
			body.WriteString(line)
			body.WriteString("\n")
			continue
		}

		verb := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(verb, "EHLO"):
			// No STARTTLS advertised: the client must cope with a relay that
			// does not offer it rather than refusing to send at all.
			write("250-fake")
			write("250 AUTH PLAIN")
		case strings.HasPrefix(verb, "HELO"):
			write("250 fake")
		case strings.HasPrefix(verb, "AUTH"):
			if f.refuse == "AUTH" {
				write("535 authentication failed")
				continue
			}
			write("235 authenticated")
		case strings.HasPrefix(verb, "MAIL FROM"):
			write("250 sender ok")
		case strings.HasPrefix(verb, "RCPT TO"):
			if f.refuse == "RCPT" {
				write("550 no such recipient")
				continue
			}
			write("250 recipient ok")
		case verb == "DATA":
			inData = true
			write("354 go ahead")
		case verb == "QUIT":
			write("221 bye")
			return
		default:
			write("250 ok")
		}
	}
}

func (f *fakeSMTP) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.received...)
}

func senderFor(f *fakeSMTP, adjust func(*mail.Options)) mail.Sender {
	opts := mail.Options{
		Host:       f.host(),
		Port:       f.port(),
		From:       "doenerstag@example.invalid",
		Encryption: mail.EncryptionNone,
		Timeout:    5 * time.Second,
	}
	if adjust != nil {
		adjust(&opts)
	}
	return mail.New(opts)
}

func TestAMessageArrivesWithItsHeadersAndBody(t *testing.T) {
	server := startFakeSMTP(t)
	sender := senderFor(server, nil)

	err := sender.Send(context.Background(), mail.Message{
		To:      "someone@example.invalid",
		Subject: "Reset your doenerstag password",
		Body:    "Here is the link:\nhttps://doener.example/reset-password/abc\n",
	})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	messages := server.messages()
	if len(messages) != 1 {
		t.Fatalf("the server received %d messages, want 1", len(messages))
	}
	got := messages[0]

	for _, want := range []string{
		"From: doenerstag@example.invalid",
		"To: someone@example.invalid",
		"Subject: Reset your doenerstag password",
		"Content-Type: text/plain; charset=UTF-8",
		// A message nobody should reply to and no auto-responder should answer.
		"Auto-Submitted: auto-generated",
		"https://doener.example/reset-password/abc",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the message does not contain %q:\n%s", want, got)
		}
	}
}

// A subject in German has to survive. Without RFC 2047 encoding it arrives as
// mojibake in half the clients that see it.
func TestANonASCIISubjectIsEncoded(t *testing.T) {
	server := startFakeSMTP(t)
	sender := senderFor(server, nil)

	err := sender.Send(context.Background(), mail.Message{
		To:      "someone@example.invalid",
		Subject: "Passwort zurücksetzen für doenerstag",
		Body:    "Grüße",
	})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	got := server.messages()[0]
	if strings.Contains(got, "Subject: Passwort zurücksetzen") {
		t.Error("the subject was sent as raw UTF-8 rather than encoded")
	}
	if !strings.Contains(got, "Subject: =?UTF-8?b?") {
		t.Errorf("the subject is not RFC 2047 encoded:\n%s", got)
	}
}

// A newline in a recipient or a subject can add headers to the message: a Bcc,
// a second From, a body of somebody else's choosing.
func TestHeaderInjectionIsRefused(t *testing.T) {
	server := startFakeSMTP(t)
	sender := senderFor(server, nil)

	cases := []mail.Message{
		{To: "a@example.invalid\r\nBcc: attacker@example.invalid", Subject: "hello", Body: "x"},
		{To: "a@example.invalid", Subject: "hello\r\nBcc: attacker@example.invalid", Body: "x"},
		{To: "a@example.invalid\nBcc: attacker@example.invalid", Subject: "hello", Body: "x"},
	}
	for _, msg := range cases {
		if err := sender.Send(context.Background(), msg); err == nil {
			t.Errorf("a header injection was accepted: %+v", msg)
		}
	}
	if got := len(server.messages()); got != 0 {
		t.Errorf("%d messages were sent despite being refused", got)
	}
}

// An installation with no mail server is a supported way to run this, so it has
// to fail in a way the caller can recognise and explain.
func TestWithoutAHostNothingIsSentAndTheReasonIsNamed(t *testing.T) {
	sender := mail.New(mail.Options{})

	err := sender.Send(context.Background(), mail.Message{
		To: "someone@example.invalid", Subject: "x", Body: "y",
	})
	if !errors.Is(err, mail.ErrNotConfigured) {
		t.Errorf("got %v, want ErrNotConfigured", err)
	}
}

// A relay that does not advertise STARTTLS still gets the mail. Refusing would
// mean no mail at all on an intranet relay that does not speak it.
func TestARelayWithoutSTARTTLSStillReceivesTheMail(t *testing.T) {
	server := startFakeSMTP(t)
	sender := senderFor(server, func(o *mail.Options) {
		o.Encryption = mail.EncryptionSTARTTLS
	})

	if err := sender.Send(context.Background(), mail.Message{
		To: "someone@example.invalid", Subject: "x", Body: "y",
	}); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if got := len(server.messages()); got != 1 {
		t.Errorf("the server received %d messages, want 1", got)
	}
}

// The error has to name the mail server, because the person reading it is
// deciding whether the problem is the application or the relay.
func TestAnUnreachableServerNamesItself(t *testing.T) {
	sender := mail.New(mail.Options{
		Host: "127.0.0.1", Port: 1, From: "a@example.invalid",
		Encryption: mail.EncryptionNone, Timeout: time.Second,
	})

	err := sender.Send(context.Background(), mail.Message{
		To: "someone@example.invalid", Subject: "x", Body: "y",
	})
	if err == nil {
		t.Fatal("sending to a closed port succeeded")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("the error does not name the mail server: %v", err)
	}
}

// A rejected recipient is the mail server's answer, not a crash.
func TestARefusedRecipientIsReported(t *testing.T) {
	server := startFakeSMTP(t)
	server.refuse = "RCPT"
	sender := senderFor(server, nil)

	err := sender.Send(context.Background(), mail.Message{
		To: "nobody@example.invalid", Subject: "x", Body: "y",
	})
	if err == nil {
		t.Fatal("a refused recipient was reported as success")
	}
	if !strings.Contains(err.Error(), "recipient") {
		t.Errorf("the error does not say what was refused: %v", err)
	}
}

// The password must not appear in an error that will be logged.
func TestAFailedAuthenticationDoesNotLeakThePassword(t *testing.T) {
	server := startFakeSMTP(t)
	server.refuse = "AUTH"
	sender := senderFor(server, func(o *mail.Options) {
		o.Username = "doenerstag"
		o.Password = "hunter2-and-then-some"
	})

	err := sender.Send(context.Background(), mail.Message{
		To: "someone@example.invalid", Subject: "x", Body: "y",
	})
	if err == nil {
		t.Fatal("a refused authentication was reported as success")
	}
	if strings.Contains(err.Error(), "hunter2-and-then-some") {
		t.Errorf("the SMTP password is in the error, and errors are logged: %v", err)
	}
}

func TestParseEncryption(t *testing.T) {
	for input, want := range map[string]mail.Encryption{
		"none":     mail.EncryptionNone,
		"starttls": mail.EncryptionSTARTTLS,
		"STARTTLS": mail.EncryptionSTARTTLS,
		"  tls  ":  mail.EncryptionTLS,
		"":         mail.EncryptionSTARTTLS,
	} {
		got, err := mail.ParseEncryption(input)
		if err != nil {
			t.Errorf("%q: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("%q became %q, want %q", input, got, want)
		}
	}

	if _, err := mail.ParseEncryption("ssl"); err == nil {
		t.Error("an unknown encryption was accepted")
	}
}
