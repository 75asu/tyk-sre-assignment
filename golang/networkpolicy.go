package main

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "tyk-sre-assignment"
)

// Workload identifies a set of pods by namespace + label selector.
type Workload struct {
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels"`
}

// IsolateRequest is the body for isolate / un-isolate: the two workloads to cut off from each other.
type IsolateRequest struct {
	A Workload `json:"a"`
	B Workload `json:"b"`
}

// canonical renders a Workload to a stable string (labels sorted) so a name hash is deterministic.
func canonical(w Workload) string {
	keys := make([]string, 0, len(w.Labels))
	for k := range w.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	s := w.Namespace + "|"
	for _, k := range keys {
		s += k + "=" + w.Labels[k] + ","
	}
	return s
}

// policyName is a deterministic DNS-1123 name for isolating target from peer (deterministic => idempotent).
func policyName(target, peer Workload) string {
	h := sha1.Sum([]byte(canonical(target) + "->" + canonical(peer)))
	return fmt.Sprintf("tyk-isolate-%x", h[:8])
}

// buildIsolationPolicy returns a default-deny NetworkPolicy selecting the target (both policyTypes + no rules = deny all).
func buildIsolationPolicy(target, peer Workload) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        policyName(target, peer),
			Namespace:   target.Namespace,
			Labels:      map[string]string{managedByLabel: managedByValue},
			Annotations: map[string]string{"tyk-sre/isolated-from": canonical(peer)},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: target.Labels},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}
}

// IsolateWorkloads creates a default-deny NetworkPolicy per workload so the two can't exchange traffic (idempotent).
func IsolateWorkloads(ctx context.Context, clientset kubernetes.Interface, a, b Workload) ([]string, error) {
	applied := make([]string, 0, 2)

	for _, pair := range [][2]Workload{{a, b}, {b, a}} {
		np := buildIsolationPolicy(pair[0], pair[1])
		ref := np.Namespace + "/" + np.Name

		_, err := clientset.NetworkingV1().NetworkPolicies(np.Namespace).Create(ctx, np, metav1.CreateOptions{})
		switch {
		case apierrors.IsAlreadyExists(err):
			applied = append(applied, ref+" (already isolated)")
		case err != nil:
			return applied, fmt.Errorf("creating network policy %s: %w", ref, err)
		default:
			applied = append(applied, ref)
		}
	}

	return applied, nil
}

// DeIsolateWorkloads removes the pair's isolation policies (idempotent).
func DeIsolateWorkloads(ctx context.Context, clientset kubernetes.Interface, a, b Workload) ([]string, error) {
	removed := make([]string, 0, 2)

	for _, pair := range [][2]Workload{{a, b}, {b, a}} {
		name := policyName(pair[0], pair[1])
		ns := pair[0].Namespace
		ref := ns + "/" + name

		err := clientset.NetworkingV1().NetworkPolicies(ns).Delete(ctx, name, metav1.DeleteOptions{})
		switch {
		case apierrors.IsNotFound(err):
			// already gone
		case err != nil:
			return removed, fmt.Errorf("deleting network policy %s: %w", ref, err)
		default:
			removed = append(removed, ref)
		}
	}

	return removed, nil
}

// isolateHandler serves POST (isolate) and DELETE (un-isolate) on /network-policies/isolate.
func (s *Server) isolateHandler(w http.ResponseWriter, r *http.Request) {
	var req IsolateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("invalid JSON body: %v", err)})
		return
	}
	if req.A.Namespace == "" || req.B.Namespace == "" || len(req.A.Labels) == 0 || len(req.B.Labels) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "both workloads require a namespace and at least one label"})
		return
	}

	switch r.Method {
	case http.MethodPost:
		applied, err := IsolateWorkloads(r.Context(), s.clientset, req.A, req.B)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "networkPolicies": applied})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"isolated": true, "networkPolicies": applied})
	case http.MethodDelete:
		removed, err := DeIsolateWorkloads(r.Context(), s.clientset, req.A, req.B)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "removed": removed})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"isolated": false, "removed": removed})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "use POST to isolate, DELETE to remove"})
	}
}
