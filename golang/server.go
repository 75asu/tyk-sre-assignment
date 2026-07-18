package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	mux.HandleFunc("/healthz", instrument("/healthz", healthHandler))
	mux.HandleFunc("/readyz", instrument("/readyz", s.readyHandler))
	mux.HandleFunc("/deployments/unhealthy", instrument("/deployments/unhealthy", s.unhealthyDeploymentsHandler))
	mux.HandleFunc("/network-policies/isolate", instrument("/network-policies/isolate", s.isolateHandler))
	mux.Handle("/metrics", promhttp.Handler()) // Prometheus scrape target
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
		slog.Info("server listening", "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// healthHandler is the process liveness check (does not touch Kubernetes).
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte("ok")); err != nil {
		slog.Error("writing healthz response", "error", err)
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("encoding json response", "error", err)
	}
}
