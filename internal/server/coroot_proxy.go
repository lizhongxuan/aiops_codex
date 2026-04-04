package server

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// allowedCorootReadPaths lists the Coroot API path prefixes that the proxy
// will forward. Everything else is rejected to enforce read-only mode.
var allowedCorootReadPaths = []string{
	"/api/v1/services",
	"/api/v1/topology",
	"/api/v1/incidents",
	"/api/v1/metrics",
	"/api/v1/status",
}

// handleCorootProxy is a session-authenticated reverse proxy to the Coroot
// backend. It filters request paths to only allow read-only endpoints and
// injects the Coroot auth token before forwarding.
func (a *App) handleCorootProxy(w http.ResponseWriter, r *http.Request, _ string) {
	if a.corootClient == nil || a.corootClient.BaseURL() == "" {
		http.Error(w, `{"error":"coroot not configured"}`, http.StatusServiceUnavailable)
		return
	}

	// Only allow GET requests (read-only mode).
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"read-only mode: only GET allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Strip the local proxy prefix to obtain the upstream path.
	const proxyPrefix = "/api/v1/coroot"
	upstreamPath := strings.TrimPrefix(r.URL.Path, proxyPrefix)
	if upstreamPath == "" {
		upstreamPath = "/"
	}

	if !isAllowedCorootPath(upstreamPath) {
		http.Error(w, `{"error":"path not allowed"}`, http.StatusForbidden)
		return
	}

	target, err := url.Parse(a.corootClient.BaseURL())
	if err != nil {
		http.Error(w, `{"error":"invalid coroot base url"}`, http.StatusInternalServerError)
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = upstreamPath
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host

			// Inject Coroot auth token.
			a.corootClient.Auth().InjectAuth(req)
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, proxyErr error) {
			log.Printf("coroot proxy error: %v", proxyErr)
			http.Error(rw, `{"error":"coroot upstream error"}`, http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

// isAllowedCorootPath checks whether the upstream path matches one of the
// read-only prefixes.
func isAllowedCorootPath(path string) bool {
	for _, prefix := range allowedCorootReadPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
