package api

import (
	"strings"
	"sync"
	"time"
)

// LoginLimiter counts failed login attempts.
//
// docs/05_auth_and_permissions.md limits only the login endpoint, and holds the
// counters in memory: they reset on restart, which is acceptable for an
// intranet tool and avoids a database write on every failed password.
//
// Two independent counters, per docs/05_auth_and_permissions.md:
//
//   - Per user name, at 10 per 15 minutes. This is the one that matters: it
//     stops somebody guessing at one account.
//   - Per client address, at 60 per 15 minutes. This catches a script working
//     through a list of names, which the per-name counter would never see
//     because each name has only one or two attempts against it.
//
// Successful logins are not counted, and a success clears the name's counter:
// somebody who mistypes their password five times and then gets it right has
// demonstrated they are who they say they are, and should not be locked out an
// hour later for two more slips.
type LoginLimiter struct {
	// PerName is the number of failures allowed per user name per window.
	PerName int
	// PerAddress is the number allowed per client address per window.
	PerAddress int
	// Window is how long a failure is remembered.
	Window time.Duration

	// Now is the clock, injectable so a test can let a window elapse.
	Now func() time.Time

	mu        sync.Mutex
	names     map[string]*counter
	addresses map[string]*counter
}

// counter is a fixed window of failures.
//
// A fixed window rather than a sliding one or a token bucket: it is a handful
// of lines, and the difference only shows at the boundary, where the worst case
// is twice the limit across two adjacent windows. For a control whose purpose
// is to slow down guessing on a trusted network, that is not worth a data
// structure.
type counter struct {
	failures int
	expires  time.Time
}

func (l *LoginLimiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Allow reports whether a login attempt may proceed, and if not, how long the
// caller must wait.
//
// The name is folded to lower case, matching the case-insensitive login: an
// attacker who alternated Anna, anna and ANNA would otherwise get three times
// the attempts.
func (l *LoginLimiter) Allow(name, address string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.expireLocked(now)

	if retry, blocked := l.blockedLocked(l.names, loginKey(name), l.PerName, now); blocked {
		return false, retry
	}
	if retry, blocked := l.blockedLocked(l.addresses, address, l.PerAddress, now); blocked {
		return false, retry
	}
	return true, 0
}

// RecordFailure counts one failed attempt.
func (l *LoginLimiter) RecordFailure(name, address string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.expireLocked(now)

	if l.names == nil {
		l.names = make(map[string]*counter, 16)
	}
	if l.addresses == nil {
		l.addresses = make(map[string]*counter, 16)
	}
	l.bumpLocked(l.names, loginKey(name), now)
	l.bumpLocked(l.addresses, address, now)
}

// RecordSuccess clears the counters a successful login should forgive.
//
// The name's counter is cleared; the address's is not. A shared address -- an
// office behind one NAT, which is the normal case here -- would otherwise let
// one person's successful login reset a counter that is protecting everybody
// else on it.
func (l *LoginLimiter) RecordSuccess(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.names, loginKey(name))
}

func (l *LoginLimiter) blockedLocked(
	counters map[string]*counter, key string, limit int, now time.Time,
) (time.Duration, bool) {
	if limit <= 0 || key == "" {
		// A limit of zero or less disables that counter, which is how an
		// operator turns one off.
		return 0, false
	}
	c, ok := counters[key]
	if !ok || c.failures < limit {
		return 0, false
	}
	return c.expires.Sub(now), true
}

// bumpLocked counts one failure against key. The map is created by the caller,
// so this takes it by value.
func (l *LoginLimiter) bumpLocked(counters map[string]*counter, key string, now time.Time) {
	if key == "" {
		return
	}
	c, ok := counters[key]
	if !ok {
		c = &counter{expires: now.Add(l.Window)}
		counters[key] = c
	}
	c.failures++
}

// expireLocked drops windows that have elapsed.
//
// Sweeping on every call keeps the maps bounded by the number of distinct names
// and addresses seen within one window, rather than by every name ever tried --
// which, for an endpoint anybody can post to, is otherwise a way to make the
// server allocate indefinitely.
func (l *LoginLimiter) expireLocked(now time.Time) {
	for _, counters := range []map[string]*counter{l.names, l.addresses} {
		for key, c := range counters {
			if !now.Before(c.expires) {
				delete(counters, key)
			}
		}
	}
}

// loginKey normalises a user name for counting.
func loginKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
