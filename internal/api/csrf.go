package api

import (
	"net/http"

	"github.com/joernott/doenerstag/internal/auth"
)

// RequireCSRF rejects a state-changing cookie-authenticated request that does
// not echo the CSRF cookie in the header.
//
// The double-submit pattern: the token is in a cookie the frontend can read and
// must be copied into X-CSRF-Token. A foreign origin can cause the browser to
// send the cookie, but the same-origin policy stops it reading the value, so it
// cannot produce the header.
//
// Two exemptions, both from docs/05_auth_and_permissions.md:
//
//   - Requests authenticated by Bearer token. They carry no ambient browser
//     credential, so there is nothing for a foreign site to abuse, and the check
//     would only make scripted access harder for no gain.
//   - Safe methods. GET, HEAD and OPTIONS must never change state, and requiring
//     a header on them would break every ordinary navigation and link.
//
// An anonymous request is also exempt, because it has no credential to abuse.
// It will be refused by the authorization helpers instead, with 2000, which is
// the accurate answer: the problem is that nobody is logged in, not that a
// token is missing.
//
// SameSite=Strict already stops most of this. The check is kept because
// SameSite is a browser-side control -- a browser that does not enforce it, or
// a request that does not come from a browser at all, is exactly the case a
// server-side check exists for.
func RequireCSRF() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !csrfRequired(r) {
				next.ServeHTTP(w, r)
				return
			}

			if !auth.EqualCSRFToken(cookieValue(r, CSRFCookieName), r.Header.Get(CSRFHeaderName)) {
				WriteError(w, r, &Error{Code: CodeInvalidCSRF})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// csrfRequired reports whether this request must carry the header.
func csrfRequired(r *http.Request) bool {
	if isSafeMethod(r.Method) {
		return false
	}
	principal := PrincipalFrom(r.Context())
	return principal != nil && !principal.ViaToken
}

// isSafeMethod reports whether the method is one that must not change state.
//
// The list is the safe methods from RFC 9110, not "everything except POST":
// naming them the other way round would silently exempt any method added later.
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
