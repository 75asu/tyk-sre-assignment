package main

import "net/http"

// readinessResponse maps a connectivity-check result to an HTTP status + body (split out so both branches are unit-testable).
func readinessResponse(version string, err error) (int, map[string]any) {
	if err != nil {
		return http.StatusServiceUnavailable, map[string]any{
			"ready":     false,
			"connected": false,
			"error":     err.Error(),
		}
	}

	return http.StatusOK, map[string]any{
		"ready":     true,
		"connected": true,
		"version":   version,
	}
}

// readyHandler serves GET /readyz: a live probe of API-server reachability, 503 when unreachable so a readiness probe drains the pod without restarting it.
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	version, err := getKubernetesVersion(s.clientset)
	status, body := readinessResponse(version, err)
	writeJSON(w, status, body)
}
