package httpapi

import (
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPExtractor extracts the client IP from an HTTP request, trusting X-Forwarded-For
// or X-Real-IP ONLY when the direct peer IP is in the configured trusted proxies list.
type IPExtractor struct {
	trustedNets []*net.IPNet
	trustedIPs  map[string]bool
}

func NewIPExtractor(trustedProxies []string) *IPExtractor {
	ext := &IPExtractor{
		trustedIPs: make(map[string]bool),
	}
	for _, raw := range trustedProxies {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "/") {
			if _, ipNet, err := net.ParseCIDR(trimmed); err == nil {
				ext.trustedNets = append(ext.trustedNets, ipNet)
			}
			continue
		}
		if ip := net.ParseIP(trimmed); ip != nil {
			ext.trustedIPs[ip.String()] = true
		}
	}
	return ext
}

func (e *IPExtractor) isTrusted(ipStr string) bool {
	if e == nil {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	if e.trustedIPs[ip.String()] {
		return true
	}
	for _, n := range e.trustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (e *IPExtractor) ClientIP(r *http.Request) string {
	peerIP := parseRemoteIP(r.RemoteAddr)
	if peerIP == "" {
		peerIP = "unknown"
	}

	if !e.isTrusted(peerIP) {
		return peerIP
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// Walk right-to-left and pick the first non-trusted valid IP
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			ip := net.ParseIP(candidate)
			if ip == nil {
				continue
			}
			if !e.isTrusted(ip.String()) {
				return ip.String()
			}
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}

	return peerIP
}

func parseRemoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
		return host
	}
	if ip := net.ParseIP(strings.TrimSpace(remoteAddr)); ip != nil {
		return ip.String()
	}
	return strings.TrimSpace(remoteAddr)
}

type ipBucket struct {
	timestamps []time.Time
}

type IPRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	nowFn  func() time.Time
	ips    map[string]*ipBucket
}

func NewIPRateLimiter(limit int, window time.Duration) *IPRateLimiter {
	if window <= 0 {
		window = time.Hour
	}
	return &IPRateLimiter{
		limit:  limit,
		window: window,
		nowFn:  time.Now,
		ips:    make(map[string]*ipBucket),
	}
}

func (rl *IPRateLimiter) Allow(ip string) (bool, int) {
	if rl == nil || rl.limit <= 0 {
		return true, 0
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFn()
	cutoff := now.Add(-rl.window)

	b, ok := rl.ips[ip]
	if !ok {
		b = &ipBucket{}
		rl.ips[ip] = b
	}

	valid := b.timestamps[:0]
	for _, ts := range b.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	b.timestamps = valid

	if len(b.timestamps) >= rl.limit {
		oldest := b.timestamps[0]
		retryAfter := int(math.Ceil(oldest.Add(rl.window).Sub(now).Seconds()))
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter
	}

	b.timestamps = append(b.timestamps, now)
	return true, 0
}
