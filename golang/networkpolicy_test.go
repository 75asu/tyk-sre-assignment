package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func workload(ns string, labels map[string]string) Workload {
	return Workload{Namespace: ns, Labels: labels}
}

func listPolicies(t *testing.T, cs *fake.Clientset, ns string) []networkingv1.NetworkPolicy {
	t.Helper()
	list, err := cs.NetworkingV1().NetworkPolicies(ns).List(context.Background(), metav1.ListOptions{})
	assert.NoError(t, err)
	return list.Items
}

func TestIsolateWorkloads(t *testing.T) {
	cs := fake.NewSimpleClientset()
	a := workload("team-a", map[string]string{"app": "a"})
	b := workload("team-b", map[string]string{"app": "b"})

	applied, err := IsolateWorkloads(context.Background(), cs, a, b)
	assert.NoError(t, err)
	assert.Len(t, applied, 2)

	// A default-deny policy in team-a selecting app=a.
	inA := listPolicies(t, cs, "team-a")
	assert.Len(t, inA, 1)
	p := inA[0]
	assert.Equal(t, map[string]string{"app": "a"}, p.Spec.PodSelector.MatchLabels)
	assert.ElementsMatch(t,
		[]networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		p.Spec.PolicyTypes)
	assert.Empty(t, p.Spec.Ingress, "no ingress rules => default-deny")
	assert.Empty(t, p.Spec.Egress, "no egress rules => default-deny")
	assert.Equal(t, managedByValue, p.Labels[managedByLabel])

	// And one in team-b selecting app=b.
	inB := listPolicies(t, cs, "team-b")
	assert.Len(t, inB, 1)
	assert.Equal(t, map[string]string{"app": "b"}, inB[0].Spec.PodSelector.MatchLabels)
}

func TestIsolateWorkloadsIdempotent(t *testing.T) {
	cs := fake.NewSimpleClientset()
	a := workload("team-a", map[string]string{"app": "a"})
	b := workload("team-b", map[string]string{"app": "b"})

	_, err := IsolateWorkloads(context.Background(), cs, a, b)
	assert.NoError(t, err)
	// second call must not error and must not create duplicates
	_, err = IsolateWorkloads(context.Background(), cs, a, b)
	assert.NoError(t, err)
	assert.Len(t, listPolicies(t, cs, "team-a"), 1)
	assert.Len(t, listPolicies(t, cs, "team-b"), 1)
}

func TestDeIsolateWorkloads(t *testing.T) {
	cs := fake.NewSimpleClientset()
	a := workload("team-a", map[string]string{"app": "a"})
	b := workload("team-b", map[string]string{"app": "b"})

	_, err := IsolateWorkloads(context.Background(), cs, a, b)
	assert.NoError(t, err)

	removed, err := DeIsolateWorkloads(context.Background(), cs, a, b)
	assert.NoError(t, err)
	assert.Len(t, removed, 2)
	assert.Len(t, listPolicies(t, cs, "team-a"), 0)
	assert.Len(t, listPolicies(t, cs, "team-b"), 0)

	// deleting again is a no-op (NotFound handled)
	_, err = DeIsolateWorkloads(context.Background(), cs, a, b)
	assert.NoError(t, err)
}

func TestIsolateHandler(t *testing.T) {
	cs := fake.NewSimpleClientset()
	srv := NewServer(cs)
	body := `{"a":{"namespace":"team-a","labels":{"app":"a"}},"b":{"namespace":"team-b","labels":{"app":"b"}}}`

	// POST isolates -> 201
	req := httptest.NewRequest(http.MethodPost, "/network-policies/isolate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Len(t, listPolicies(t, cs, "team-a"), 1)

	// DELETE removes -> 200
	req = httptest.NewRequest(http.MethodDelete, "/network-policies/isolate", strings.NewReader(body))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, listPolicies(t, cs, "team-a"), 0)

	// Missing labels -> 400
	bad := `{"a":{"namespace":"team-a"},"b":{"namespace":"team-b","labels":{"app":"b"}}}`
	req = httptest.NewRequest(http.MethodPost, "/network-policies/isolate", strings.NewReader(bad))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
