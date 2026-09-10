package api

import (
	"testing"

	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/config"
)

// Two questions that used to be one.
//
// "Does this process speak TLS" and "is the browser's side of the connection
// encrypted" were the same boolean until a reverse proxy was put in front, and
// then they were not. Getting it wrong in either direction is quiet: a server
// that serves TLS when it should not fails to start, which is loud, but a
// server that drops the Secure flag from its session cookie serves a working
// site that has stopped protecting the session.
func TestWhatCountsAsSecure(t *testing.T) {
	for _, tc := range []struct {
		name           string
		noHTTPS        bool
		behindTLSProxy bool
		secure         bool
	}{
		{
			name:   "serving TLS itself",
			secure: true,
		},
		{
			name:    "plain HTTP, nothing in front: a development server",
			noHTTPS: true,
			secure:  false,
		},
		{
			name:           "plain HTTP behind a proxy that terminates TLS",
			noHTTPS:        true,
			behindTLSProxy: true,
			secure:         true,
		},
		{
			// Nonsense, but harmless nonsense: it is already true.
			name:           "serving TLS and claiming a proxy",
			behindTLSProxy: true,
			secure:         true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := zerolog.Nop()
			cfg := &config.Config{}
			cfg.Server.NoHTTPS = tc.noHTTPS
			cfg.Server.BehindTLSProxy = tc.behindTLSProxy
			cfg.Session.JWTSecret = "a secret long enough to build a signer from"

			s, err := NewServer(ServerOptions{Config: cfg, Logger: &logger})
			if err != nil {
				t.Fatalf("building the server: %v", err)
			}

			if s.secure != tc.secure {
				t.Errorf("secure is %v, want %v", s.secure, tc.secure)
			}
			// And the other half: what it actually listens with follows
			// --no-https alone, so --behind-tls-proxy cannot make a server
			// reach for a certificate that is not there.
			if servesTLS := !s.cfg.Server.NoHTTPS; servesTLS == tc.noHTTPS {
				t.Errorf("serving TLS is %v with no_https=%v", servesTLS, tc.noHTTPS)
			}
		})
	}
}
