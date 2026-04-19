package server

import (
	"net/http"
	"net/url"
)

// CorootConfigResponse is the JSON payload returned by the Coroot config API.
type CorootConfigResponse struct {
	Configured bool   `json:"configured"`
	BaseURL    string `json:"baseUrl,omitempty"`
	IframeMode bool   `json:"iframeMode"`
}

// handleCorootConfig returns the current Coroot integration configuration
// status. The base URL is sanitised to expose only the host/domain — no path,
// query parameters or credentials are leaked.
//
// GET /api/v1/coroot/config
func (a *App) handleCorootConfig(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	resp := CorootConfigResponse{}

	if a.corootClient != nil && a.corootClient.BaseURL() != "" {
		resp.Configured = true
		resp.IframeMode = true

		// Extract just the scheme + host so we never expose path, query or
		// userinfo from the configured base URL.
		if parsed, err := url.Parse(a.corootClient.BaseURL()); err == nil {
			resp.BaseURL = parsed.Scheme + "://" + parsed.Host
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
