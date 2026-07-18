package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func replicas(n int32) *int32 { return &n }

func deployment(ns, name string, desired *int32, ready, available int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       appsv1.DeploymentSpec{Replicas: desired},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: ready, AvailableReplicas: available},
	}
}

func TestCheckDeploymentHealth(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("default", "healthy", replicas(3), 3, 3),     // ok: 3/3 ready
		deployment("default", "degraded", replicas(3), 1, 1),    // unhealthy: 1/3 ready
		deployment("shop", "scaled-to-zero", replicas(0), 0, 0), // ok: intentionally 0
		deployment("shop", "nil-replicas", nil, 0, 0),           // unhealthy: default 1, 0 ready
	)

	// All namespaces.
	all, err := CheckDeploymentHealth(context.Background(), clientset, "")
	assert.NoError(t, err)
	assert.Len(t, all, 2)

	names := map[string]bool{}
	for _, d := range all {
		names[d.Name] = true
	}
	assert.True(t, names["degraded"])
	assert.True(t, names["nil-replicas"])
	assert.False(t, names["healthy"])
	assert.False(t, names["scaled-to-zero"])

	// Namespace filter scopes to one namespace.
	def, err := CheckDeploymentHealth(context.Background(), clientset, "default")
	assert.NoError(t, err)
	assert.Len(t, def, 1)
	assert.Equal(t, "degraded", def[0].Name)
	assert.Equal(t, int32(3), def[0].DesiredReplicas)
	assert.Equal(t, int32(1), def[0].ReadyReplicas)
}

func TestUnhealthyDeploymentsHandler(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("default", "degraded", replicas(2), 0, 0),
	)
	srv := NewServer(clientset)

	req := httptest.NewRequest(http.MethodGet, "/deployments/unhealthy", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req) // goes through the real router

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body struct {
		UnhealthyCount int                `json:"unhealthyCount"`
		Unhealthy      []DeploymentHealth `json:"unhealthy"`
	}
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 1, body.UnhealthyCount)
	assert.Equal(t, "degraded", body.Unhealthy[0].Name)
	assert.Equal(t, int32(2), body.Unhealthy[0].DesiredReplicas)
	assert.Equal(t, int32(0), body.Unhealthy[0].ReadyReplicas)
}
