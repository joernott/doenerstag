package api

import (
	"net/http"
	"strconv"
	"time"
)

// Cookie and header names, per docs/05_auth_and_permissions.md.
const (
	// SessionCookieName carries the JWT. HttpOnly, so script cannot read it.
	SessionCookieName = "doener_session"

	// CSRFCookieName carries the CSRF token. Deliberately not HttpOnly: the
	// frontend has to read it to put it in the header, which is what makes the
	// double-submit pattern work.
	CSRFCookieName = "doener_csrf"

	// SessionEndedCookieName carries the reason a session cookie was refused,
	// from the request that detected it to the next call to /auth/session.
	// HttpOnly: the frontend is told through that endpoint, not by reading
	// this.
	SessionEndedCookieName = "doener_session_ended"

	// CSRFHeaderName is where that value must come back.
	CSRFHeaderName = "X-CSRF-Token"
)

// setSessionCookies issues both cookies for a new session.
//
// secure is false only under --no-https. Sending Secure over plain HTTP would
// mean the browser never returns the cookie, so a development server would
// appear to log in and then immediately not be logged in.
// gosec flags both calls below as possibly missing Secure, because the value is
// a variable it cannot evaluate rather than a literal true. It is not missing:
// it is false exactly when the server was started with --no-https, and hard-
// coding true there would mean a development server appears to log in and is
// then immediately not logged in, because the browser never returns the cookie
// over plain HTTP. ListenAndServe already warns loudly in that mode.
func setSessionCookies(w http.ResponseWriter, token, csrf string, lifetime time.Duration, secure bool) {
	maxAge := int(lifetime.Seconds())

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is conditional on --no-https; see above
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // as above, and HttpOnly is false on purpose
		Name:  CSRFCookieName,
		Value: csrf,
		Path:  "/",
		// Same lifetime as the session it belongs to: a CSRF cookie that
		// outlived its session would be sent with requests that have no
		// session to protect.
		MaxAge: maxAge,
		// Readable by script, on purpose. It is not a credential on its own --
		// it proves only that the request came from a page that could read this
		// origin's cookies.
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// cookieValue reads a cookie, returning empty when it is absent.
func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// clearSessionCookies expires both cookies.
//
// The attributes must match the ones they were set with. A browser keys a
// cookie on name, domain and path, so a deletion that differs in Path or Secure
// creates a second, already-expired cookie and leaves the original in place --
// which would look exactly like a logout that did nothing.
func clearSessionCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{SessionCookieName, CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is conditional on --no-https, as above
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == SessionCookieName,
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

// SessionEndedLifetime is how long the notice below survives.
//
// Long enough for the page load that follows to collect it, short enough that a
// browser left closed for a week is not greeted with news about a session it
// has forgotten.
const SessionEndedLifetime = 5 * time.Minute

// setSessionEndedCookie records why a session cookie was refused.
//
// The reason has to be written down at the moment it is detected, not looked up
// again later, because looking it up again gives a different answer: the
// request that finds a session past its idle timeout also deletes the row, so
// the next request finds no row and concludes "superseded by a newer login" --
// which is a confusing thing to tell somebody whose session simply timed out
// while they slept.
//
// HttpOnly, because the frontend never reads it directly. GET /auth/session
// reads it server-side, reports it in a form the frontend already knows how to
// translate, and clears it, so the news is delivered exactly once.
func setSessionEndedCookie(w http.ResponseWriter, code Code, secure bool) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is conditional on --no-https, as above
		Name:     SessionEndedCookieName,
		Value:    strconv.Itoa(int(code)),
		Path:     "/",
		MaxAge:   int(SessionEndedLifetime.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearSessionEndedCookie expires the notice, once it has been delivered.
func clearSessionEndedCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is conditional on --no-https, as above
		Name:     SessionEndedCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// sessionEndedFromCookie reads the notice, if there is a usable one.
//
// An unregistered or unparseable value is ignored rather than reported: it did
// not come from here, and inventing an error code out of whatever a client put
// in a cookie would put arbitrary text in front of a user.
func sessionEndedFromCookie(r *http.Request) (Code, bool) {
	raw := cookieValue(r, SessionEndedCookieName)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	code := Code(n)
	if !code.Registered() {
		return 0, false
	}
	return code, true
}
