package server

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// loginLimiter slows down password guessing: after max failed logins from one
// client IP within window, that IP gets 429 until the window ends. Behind a
// reverse proxy every client shares the proxy's IP, so a guessing attacker can
// lock everyone out for at most one window.
type loginLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string]*failureWindow
}

type failureWindow struct {
	start time.Time
	count int
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, now: time.Now, failures: map[string]*failureWindow{}}
}

// retryAfter reports how long ip must wait, or 0 if it may try again.
func (l *loginLimiter) retryAfter(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.failures[ip]
	if !ok {
		return 0
	}
	left := l.window - l.now().Sub(f.start)
	if left <= 0 {
		delete(l.failures, ip)
		return 0
	}
	if f.count >= l.max {
		return left
	}
	return 0
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.failures) > 10000 { // drop expired windows so the map stays small
		for k, f := range l.failures {
			if now.Sub(f.start) >= l.window {
				delete(l.failures, k)
			}
		}
	}
	f, ok := l.failures[ip]
	if !ok || now.Sub(f.start) >= l.window {
		l.failures[ip] = &failureWindow{start: now, count: 1}
		return
	}
	f.count++
}

func (l *loginLimiter) succeed(ip string) {
	l.mu.Lock()
	delete(l.failures, ip)
	l.mu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Round(time.Second)/time.Second)+1))
	http.Error(w, "too many failed logins; try again later", http.StatusTooManyRequests)
}
