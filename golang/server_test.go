package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"
)

// TestServerGracefulShutdown starts the server, confirms it serves, then cancels the
// context (SIGTERM equivalent) and asserts it drains and returns nil rather than erroring.
func TestServerGracefulShutdown(t *testing.T) {
	s := NewServer(fake.NewSimpleClientset())
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start(ctx, "127.0.0.1:18099") }()

	var err error
	for range 50 { // wait for the listener to come up
		var resp *http.Response
		if resp, err = http.Get("http://127.0.0.1:18099/healthz"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.NoError(t, err, "server should be serving before shutdown")

	cancel() // trigger graceful shutdown

	select {
	case err := <-errCh:
		assert.NoError(t, err, "graceful shutdown should drain and return nil")
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5s of context cancel")
	}
}
