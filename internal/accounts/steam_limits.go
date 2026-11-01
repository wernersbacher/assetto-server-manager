package accounts

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

const (
	// steamLoginsPerIP is how many requests to /login/steam one IP address may make per steamLoginWindow (one login
	// takes two: the redirect to Steam and the answer of Steam).
	steamLoginsPerIP  = 30
	steamLoginWindow  = 10 * time.Minute
	steamSignupsLimit = 20
	steamSignupWindow = time.Hour
)

// ErrSteamSignupLimit is returned if too many accounts were created by Steam logins in the last hour.
var ErrSteamSignupLimit = errors.New("servermanager: too many accounts were created with Steam recently, try again later")

// rateLimiter allows a number of events per key within a sliding window. It keeps the times in memory only.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	events map[string][]time.Time
}

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, now: now, events: make(map[string][]time.Time)}
}

// allow records an event of the key and tells whether it is within the limit. Refused events are not counted.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)

	// forget the old events of all keys, so that the memory does not grow with the number of visitors
	for k, times := range l.events {
		kept := times[:0]

		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}

		if len(kept) == 0 {
			delete(l.events, k)
		} else {
			l.events[k] = kept
		}
	}

	if len(l.events[key]) >= l.limit {
		return false
	}

	l.events[key] = append(l.events[key], now)

	return true
}

// clientIP is the IP address of the request (the RealIP middleware has put the one of a proxy header into
// RemoteAddr already).
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}

	return r.RemoteAddr
}

// limitSteamLogins refuses requests of IPs that make too many Steam login requests. It does not touch the session:
// the request can come from another site.
func (ah *AccountHandler) limitSteamLogins(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ah.accountManager.loginLimiter().allow(clientIP(r)) {
			w.Header().Set("Retry-After", "600")
			http.Error(w, smweb.T(r, "flash.accounts.steam_rate_limited"), http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

func (am *AccountManager) loginLimiter() *rateLimiter {
	am.limitMutex.Lock()
	defer am.limitMutex.Unlock()

	if am.loginLimit == nil {
		am.loginLimit = newRateLimiter(steamLoginsPerIP, steamLoginWindow, am.clock)
	}

	return am.loginLimit
}

// allowSteamSignup counts a new account that was created by a Steam login and tells whether it is within the limit.
func (am *AccountManager) allowSteamSignup() bool {
	am.limitMutex.Lock()

	if am.signupLimit == nil {
		am.signupLimit = newRateLimiter(steamSignupsLimit, steamSignupWindow, am.clock)
	}

	limiter := am.signupLimit

	am.limitMutex.Unlock()

	return limiter.allow("signup")
}
