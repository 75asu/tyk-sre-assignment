package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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
	mux.HandleFunc("/network-policies/isolate", s.isolateHandler)
	return mux
}

// Start serves until ctx is cancelled (e.g. SIGTERM), then drains in-flight requests (graceful shutdown).
func (s *Server) Start(ctx context.Context, listenAddr string) error {
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,  // slow-loris protection
		ReadTimeout:       15 * time.Second, // whole-request read bound
		WriteTimeout:      15 * time.Second, // handler + response write bound
		IdleTimeout:       60 * time.Second, // keep-alive reaping
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("Server listening on %s\n", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Println("shutdown signal received, draining connections...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
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
