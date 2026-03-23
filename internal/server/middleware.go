package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

)

// NOTE(localhost-app): Huginn binds exclusively to 127.0.0.1. All security
// measures in this file are defence-in-depth guards for the embedded browser/
// WebView context, NOT primary security boundaries. Specifically:
//
//   - Rate limiters (authFailLimiter, flowRateLimiter, endpointRateLimiter,
//     wsRateAllow) are usability guards that prevent accidental tight-loop
//     clients and log-spam from @mention floods. They should not be tightened
//     under the assumption that external traffic is possible, because it is not.
//     If future remote-access support is added, re-evaluate all limits.
//
//   - Security headers (HSTS, CSP) are set as belt-and-suspenders for the
//     WebView but are not a meaningful attack surface over localhost HTTP.
//     HSTS is intentionally omitted because HTTPS is not required on localhost.
//     CSP is permissive by design to accommodate the Vue.js web UI.
//
// This comment exists so future security audits do not flag these decisions
// as gaps without understanding the deployment model.

// requestIDHeader is the HTTP header added to every response for request tracing.
const requestIDHeader = "X-Request-ID"

// requestIDContextKey is the key used to store the request ID in a context.
type requestIDContextKey struct{}

// RequestIDFromContext retrieves the request ID stored in ctx by requestIDMiddleware.
// Returns "" if no request ID is present.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDContextKey{}).(string)
	return v
}

// requestIDMiddleware generates a random request ID for each request, stores it
// in both the response header and the request context for downstream handlers.
func requestIDMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, id)
		next(w, r.WithContext(ctx))
	}
}

// authMiddleware validates the Bearer token on all API requests.
// Token must match the server's auth token.
// Allows access if ?token=<tok> or Authorization: Bearer <tok>
// Uses constant-time comparison to prevent timing-based token oracle attacks.
// After authFailMaxPerMinute failures from the same IP within authFailWindow,
// subsequent requests receive HTTP 429 with a Retry-After header.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := extractClientIP(r)

		// Check if the IP is already rate-limited before performing auth.
		if s.authLimiter != nil && s.authLimiter.isBlocked(ip) {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(authFailWindow.Seconds())))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"too many auth failures, slow down"}` + "\n")) //nolint:errcheck
			return
		}

		tok := extractToken(r)
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
			slog.Warn("auth failed", "path", r.URL.Path, "request_id", r.Header.Get("X-Request-ID"))
			// Record failure and check if this pushes the IP over the limit.
			if s.authLimiter != nil && s.authLimiter.recordFailure(ip) {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(authFailWindow.Seconds())))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"too many auth failures, slow down"}` + "\n")) //nolint:errcheck
				return
			}
			// Return JSON (not text/plain) so clients can parse error responses uniformly.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}` + "\n")) //nolint:errcheck
			return
		}
		next(w, r)
	}
}

func extractToken(r *http.Request) string {
	// Check Authorization: Bearer <token>
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Check ?token=<token> (for WebSocket upgrades and browser testing)
	return r.URL.Query().Get("token")
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// loggingMiddleware wraps a handler and logs each HTTP request.
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next(rw, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", r.Header.Get("X-Request-ID"),
			"bytes_in", r.ContentLength,
		)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher so SSE / streaming handlers work correctly
// through the logging middleware.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// authFailLimiter tracks per-IP authentication failure counts using a sliding
// window to detect brute-force attempts. Once an IP exceeds authFailMaxPerMinute
// failed auth attempts within authFailWindow, subsequent requests from that IP
// receive HTTP 429 until the window expires.
const (
	authFailMaxPerMinute = 10
	authFailWindow       = time.Minute
)

type authFailLimiter struct {
	mu      sync.Mutex
	window  map[string][]time.Time
	clockFn func() time.Time
}

func newAuthFailLimiter() *authFailLimiter {
	return newAuthFailLimiterWithClock(time.Now)
}

func newAuthFailLimiterWithClock(fn func() time.Time) *authFailLimiter {
	return &authFailLimiter{
		window:  make(map[string][]time.Time),
		clockFn: fn,
	}
}

// recordFailure records an auth failure for ip and returns true if the IP has
// now exceeded authFailMaxPerMinute failures within authFailWindow.
func (a *authFailLimiter) recordFailure(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.clockFn()
	cutoff := now.Add(-authFailWindow)
	times := a.evict(ip, cutoff)
	times = append(times, now)
	a.window[ip] = times
	return len(times) > authFailMaxPerMinute
}

// isBlocked returns true if ip currently exceeds the failure threshold.
func (a *authFailLimiter) isBlocked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.clockFn()
	cutoff := now.Add(-authFailWindow)
	times := a.evict(ip, cutoff)
	a.window[ip] = times
	return len(times) > authFailMaxPerMinute
}

// evict removes entries older than cutoff and returns the pruned slice.
// Caller must hold a.mu.
func (a *authFailLimiter) evict(ip string, cutoff time.Time) []time.Time {
	times := a.window[ip]
	j := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[j] = t
			j++
		}
	}
	return times[:j]
}

// flowRateLimiter enforces per-IP sliding window rate limiting for OAuth flows.
const (
	maxOAuthFlowsPerIP  = 5
	oAuthFlowRateWindow = time.Minute
)

type flowRateLimiter struct {
	mu      sync.Mutex
	window  map[string][]time.Time
	clockFn func() time.Time // injectable for testing; defaults to time.Now
}

func newFlowRateLimiter() *flowRateLimiter {
	return newFlowRateLimiterWithClock(time.Now)
}

// newFlowRateLimiterWithClock creates a flowRateLimiter with a custom clock,
// allowing tests to control time without real sleeps.
func newFlowRateLimiterWithClock(clock func() time.Time) *flowRateLimiter {
	return &flowRateLimiter{window: make(map[string][]time.Time), clockFn: clock}
}

func (f *flowRateLimiter) allow(ip string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clockFn()
	cutoff := now.Add(-oAuthFlowRateWindow)
	times := f.window[ip]
	// Evict stale entries
	j := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[j] = t
			j++
		}
	}
	times = times[:j]
	if len(times) >= maxOAuthFlowsPerIP {
		f.window[ip] = times
		return false
	}
	f.window[ip] = append(times, now)
	return true
}

// extractClientIP returns the client IP from the request, correctly handling
// IPv6 addresses (which include colons) by using net.SplitHostPort.
// Falls back to the raw RemoteAddr if parsing fails.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header for proxied requests first.
	// The server binds to 127.0.0.1 only, so this is low-risk, but we still
	// sanitise: take only the first (leftmost) entry.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// endpointRateLimiter is a generic per-IP sliding-window rate limiter for
// HTTP endpoints. It is safe for concurrent use.
type endpointRateLimiter struct {
	mu     sync.Mutex
	window map[string][]time.Time
	limit  int
	dur    time.Duration
}

// newEndpointRateLimiter creates a sliding-window limiter that allows at most
// limit requests per IP within the given window duration.
func newEndpointRateLimiter(limit int, dur time.Duration) *endpointRateLimiter {
	return &endpointRateLimiter{
		window: make(map[string][]time.Time),
		limit:  limit,
		dur:    dur,
	}
}

func (e *endpointRateLimiter) allow(ip string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-e.dur)
	times := e.window[ip]
	// Evict stale entries outside the window.
	j := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[j] = t
			j++
		}
	}
	times = times[:j]
	if len(times) >= e.limit {
		e.window[ip] = times
		return false
	}
	e.window[ip] = append(times, now)
	return true
}

// rateLimitMiddleware wraps a handler with per-IP sliding-window rate limiting.
// The limiter is resolved via getLimiter at each request so that tests can
// replace the limiter field on the Server after route registration.
// On excess it returns HTTP 429 with a Retry-After header and logs the event.
func (s *Server) rateLimitMiddleware(getLimiter func() *endpointRateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limiter := getLimiter()
		ip := extractClientIP(r)
		if !limiter.allow(ip) {
			slog.Warn("rate limit exceeded",
				"ip", ip,
				"path", r.URL.Path,
				"method", r.Method,
			)
			retryAfter := int(limiter.dur.Seconds())
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			jsonError(w, http.StatusTooManyRequests, "rate limit exceeded, slow down")
			return
		}
		next(w, r)
	}
}

// idSegmentRe matches URL path segments that look like opaque IDs (≥16 chars,
// alphanumeric with common ID characters).  These are replaced with ":id" in
// metric labels to prevent cardinality explosion.
var idSegmentRe = regexp.MustCompile(`/[A-Za-z0-9_-]{16,}`)

// sanitizePath replaces long opaque ID-like path segments with ":id" so that
// HTTP metrics are grouped by route shape rather than individual resource IDs.
func sanitizePath(path string) string {
	return idSegmentRe.ReplaceAllString(path, "/:id")
}

// loggingMiddlewareWithStats wraps a handler, logs each request, and records
// request duration in the Server's stats registry as "http.request.duration_ms".
func (s *Server) loggingMiddlewareWithStats(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next(rw, r)
		elapsed := time.Since(start)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", elapsed.Milliseconds(),
			"request_id", RequestIDFromContext(r.Context()),
		)
		if s.statsReg != nil {
			s.statsReg.Collector().Histogram("http.request.duration_ms", float64(elapsed.Milliseconds()),
				"path", sanitizePath(r.URL.Path),
				"method", r.Method,
				"status", fmt.Sprintf("%d", rw.status),
			)
		}
	}
}

// csrfMiddleware enforces CSRF protection for state-mutating requests (POST/PUT/DELETE/PATCH).
//
// Strategy: validate the Origin header against an allowlist of permitted origins.
// This is correct and complete for a localhost-only server:
//
//   - Browser requests include an Origin header on cross-site fetches →
//     if the origin is not in the allowlist, the request is rejected (403).
//   - Non-browser clients (CLI, relay satellite, MCP) do NOT send an Origin header →
//     they bypass the check entirely.
//   - GET/HEAD/OPTIONS are safe methods and pass through unconditionally.
//
// allowedOrigins should be built from the configured bind address + port at startup,
// e.g. {"http://localhost:8080": true, "http://127.0.0.1:8080": true}.
func csrfMiddleware(allowedOrigins map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				// No Origin header → non-browser client (CLI, relay, MCP) → allow.
				next.ServeHTTP(w, r)
				return
			}
			if allowedOrigins[origin] {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		})
	}
}

// securityHeadersMiddleware adds common security headers to every response.
// These headers reduce attack surface for XSS, clickjacking, and MIME sniffing.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Standard defence-in-depth headers.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Cache-Control", "no-store")
		// X-XSS-Protection: 0 disables the legacy browser XSS filter. Modern
		// browsers ignore this header; setting it to 1 can actually introduce
		// vulnerabilities in older browsers. CSP below is the correct mitigation.
		w.Header().Set("X-XSS-Protection", "0")

		// Content-Security-Policy: permissive for localhost WebView + Vue.js dev mode.
		// 'unsafe-inline' and 'unsafe-eval' are required by the bundled Vue.js UI.
		// NOTE(localhost-app): tighten this CSP if Huginn is ever served remotely.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self' 'unsafe-inline' 'unsafe-eval' "+
				"ws://localhost wss://localhost ws://127.0.0.1 wss://127.0.0.1; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self' ws://localhost wss://localhost ws://127.0.0.1 wss://127.0.0.1;")

		// Restrict browser feature access. Camera/microphone/geolocation are not
		// used by Huginn and should not be available to any injected scripts.
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		// Cross-Origin isolation headers. These enable SharedArrayBuffer and
		// performance.measureUserAgentSpecificMemory() if needed in future.
		// NOTE: COEP may break loading cross-origin resources without CORP headers.
		// Monitor and relax if third-party embeds are needed.
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")

		next.ServeHTTP(w, r)
	})
}
