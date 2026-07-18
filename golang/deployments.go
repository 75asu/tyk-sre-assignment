package main

import (
	"context"
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DeploymentHealth reports a Deployment whose ready pods don't match its spec.
type DeploymentHealth struct {
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	DesiredReplicas   int32  `json:"desiredReplicas"`
	ReadyReplicas     int32  `json:"readyReplicas"`
	AvailableReplicas int32  `json:"availableReplicas"`
}

// CheckDeploymentHealth returns Deployments whose readyReplicas != desired spec.replicas (namespace "" = all namespaces).
func CheckDeploymentHealth(ctx context.Context, clientset kubernetes.Interface, namespace string) ([]DeploymentHealth, error) {
	deployments, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}

	unhealthy := make([]DeploymentHealth, 0)
	for _, d := range deployments.Items {
		desired := int32(1) // spec.replicas is a pointer; Kubernetes defaults unset to 1
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}

		if d.Status.ReadyReplicas != desired {
			unhealthy = append(unhealthy, DeploymentHealth{
				Namespace:         d.Namespace,
				Name:              d.Name,
				DesiredReplicas:   desired,
				ReadyReplicas:     d.Status.ReadyReplicas,
				AvailableReplicas: d.Status.AvailableReplicas,
			})
		}
	}

	return unhealthy, nil
}

// unhealthyDeploymentsHandler serves GET /deployments/unhealthy (optional ?namespace=); empty result is 200, not an error.
func (s *Server) unhealthyDeploymentsHandler(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")

	unhealthy, err := CheckDeploymentHealth(r.Context(), s.clientset, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	scope := "*"
	if namespace != "" {
		scope = namespace
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"namespace":      scope,
		"unhealthyCount": len(unhealthy),
		"unhealthy":      unhealthy,
	})
}
