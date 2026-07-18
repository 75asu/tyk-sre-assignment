package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/client-go/kubernetes"
)

// Server holds the dependencies shared by the HTTP handlers (kept off a global so handlers are testable with a fake clientset).
type Server struct {
	clientset kubernetes.Interface
}

func NewServer(clientset kubernetes.Interface) *Server {
	return &Server{clientset: clientset}
}

// Handler wires the routes and returns the http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", s.readyHandler)
	mux.HandleFunc("/deployments/unhealthy", s.unhealthyDeploymentsHandler)
	return mux
}

// Start binds the listen address and serves until terminated or error.
func (s *Server) Start(listenAddr string) error {
	fmt.Printf("Server listening on %s\n", listenAddr)
	return http.ListenAndServe(listenAddr, s.Handler())
}

// healthHandler is the process liveness check (does not touch Kubernetes).
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte("ok")); err != nil {
		fmt.Println("failed writing to response")
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		fmt.Printf("failed encoding response: %v\n", err)
	}
}
