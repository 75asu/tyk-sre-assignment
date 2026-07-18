package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"
)

// TestMetricsEndpoint confirms /metrics is served and records a request after one is made.
func TestMetricsEndpoint(t *testing.T) {
	srv := httptest.NewServer(NewServer(fake.NewSimpleClientset()).Handler())
	defer srv.Close()

	// generate one request so the counter is non-empty
	resp, err := http.Get(srv.URL + "/healthz")
	assert.NoError(t, err)
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/metrics")
	assert.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "http_requests_total")
	assert.Contains(t, string(body), `route="/healthz"`)
}
