package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/version"
	disco "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReadinessResponse(t *testing.T) {
	// Reachable -> 200, connected true, version echoed.
	status, body := readinessResponse("v1.29.0", nil)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, true, body["connected"])
	assert.Equal(t, true, body["ready"])
	assert.Equal(t, "v1.29.0", body["version"])

	// Unreachable -> 503, connected false, error surfaced.
	status, body = readinessResponse("", errors.New("dial tcp 10.0.0.1:443: connect: connection refused"))
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, false, body["connected"])
	assert.Equal(t, false, body["ready"])
	assert.Contains(t, body["error"].(string), "connection refused")
}

func TestReadyHandler(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.Discovery().(*disco.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.29.0-fake"}
	srv := NewServer(clientset)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req) // through the real router

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]any
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, true, body["connected"])
	assert.Equal(t, "v1.29.0-fake", body["version"])
}
