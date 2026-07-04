package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestReadyHandler(t *testing.T) {
	// Reachable dependency → 200 ready.
	rr := httptest.NewRecorder()
	readyHandler(fakePinger{}).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "ready" {
		t.Errorf("healthy: code=%d body=%q", rr.Code, rr.Body.String())
	}

	// Unreachable dependency → 503, so a load balancer drains this instance.
	rr = httptest.NewRecorder()
	readyHandler(fakePinger{err: errors.New("connection refused")}).
		ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("db down: code=%d, want 503", rr.Code)
	}
}
