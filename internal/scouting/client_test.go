package scouting

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// capturedRequest snapshots the pieces of an incoming request we want to
// assert against. We store it via atomic.Pointer because the httptest server
// handler runs on a separate goroutine.
type capturedRequest struct {
	Method     string
	Path       string
	AuthHeader string
	EsbURL     string
	Origin     string
	Referer    string
}

func TestClientSendsAuthHeaders(t *testing.T) {
	var captured atomic.Pointer[capturedRequest]

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cr := &capturedRequest{
			Method:     r.Method,
			Path:       r.URL.Path,
			AuthHeader: r.Header.Get("Authorization"),
			EsbURL:     r.Header.Get("x-esb-url"),
			Origin:     r.Header.Get("Origin"),
			Referer:    r.Header.Get("Referer"),
		}
		captured.Store(cr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"adventureId":1}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "my-jwt-token", WithRetryBaseDelay(1*time.Millisecond))

	esbTarget := "https://advancements.scouting.org/roster"
	var out AdventureRequirements
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := client.Get(ctx, "/some/path", esbTarget, &out); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	cr := captured.Load()
	if cr == nil {
		t.Fatalf("handler did not capture a request")
	}

	if got, want := cr.Method, http.MethodGet; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := cr.Path, "/some/path"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := cr.AuthHeader, "Bearer my-jwt-token"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	wantEsb := base64.StdEncoding.EncodeToString([]byte(esbTarget))
	if got := cr.EsbURL; got != wantEsb {
		t.Errorf("x-esb-url = %q, want %q", got, wantEsb)
	}
	if got, want := cr.Origin, "https://advancements.scouting.org"; got != want {
		t.Errorf("Origin = %q, want %q", got, want)
	}
	if got, want := cr.Referer, "https://advancements.scouting.org/"; got != want {
		t.Errorf("Referer = %q, want %q", got, want)
	}
}

func TestClientUnmarshalsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"adventureId":140,"adventureName":"My Family"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token", WithRetryBaseDelay(1*time.Millisecond))

	var out AdventureRequirements
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := client.Get(ctx, "/adventures/140", "https://advancements.scouting.org/roster", &out); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	if got, want := out.AdventureId, 140; got != want {
		t.Errorf("AdventureId = %d, want %d", got, want)
	}
	if got, want := out.AdventureName, "My Family"; got != want {
		t.Errorf("AdventureName = %q, want %q", got, want)
	}
}

func TestClientReturnsErrTokenExpiredOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token", WithRetryBaseDelay(1*time.Millisecond))

	var out AdventureRequirements
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := client.Get(ctx, "/foo", "https://advancements.scouting.org/roster", &out)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("errors.Is(err, ErrTokenExpired) = false, want true (err=%v)", err)
	}
}

func TestClientRetriesOn429(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"adventureId":140}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token",
		WithRetryBaseDelay(1*time.Millisecond),
		WithMaxRetries(5),
	)

	var out AdventureRequirements
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := client.Get(ctx, "/foo", "https://advancements.scouting.org/roster", &out); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got, want := count.Load(), int32(3); got != want {
		t.Errorf("request count = %d, want %d", got, want)
	}
	if got, want := out.AdventureId, 140; got != want {
		t.Errorf("AdventureId = %d, want %d", got, want)
	}
}

func TestClientRetriesOn5xx(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"adventureId":140}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token",
		WithRetryBaseDelay(1*time.Millisecond),
		WithMaxRetries(5),
	)

	var out AdventureRequirements
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := client.Get(ctx, "/foo", "https://advancements.scouting.org/roster", &out); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got, want := count.Load(), int32(3); got != want {
		t.Errorf("request count = %d, want %d", got, want)
	}
	if got, want := out.AdventureId, 140; got != want {
		t.Errorf("AdventureId = %d, want %d", got, want)
	}
}

func TestClientDoesNotRetryOn400(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token",
		WithRetryBaseDelay(1*time.Millisecond),
		WithMaxRetries(5),
	)

	var out AdventureRequirements
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := client.Get(ctx, "/foo", "https://advancements.scouting.org/roster", &out)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if errors.Is(err, ErrTokenExpired) {
		t.Errorf("unexpected ErrTokenExpired for 400: %v", err)
	}
	if got, want := count.Load(), int32(1); got != want {
		t.Errorf("request count = %d, want %d (should not retry on 400)", got, want)
	}
}

func TestClientRespectsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token", WithRetryBaseDelay(1*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out AdventureRequirements
	err := client.Get(ctx, "/foo", "https://advancements.scouting.org/roster", &out)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected context error, got nil")
	}
	// Make sure the client actually honored the context (deadline ~10ms);
	// 200ms gives plenty of slack for slow CI without letting the 500ms
	// handler sleep complete.
	if elapsed > 200*time.Millisecond {
		t.Errorf("Get took %v, expected to return promptly after ctx cancel", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.DeadlineExceeded or context.Canceled", err)
	}
}

// TestClientGivesUpAfterMaxRetries: server always returns 503. With
// WithMaxRetries(3), the client should make exactly 3 total attempts
// (interpretation: MaxRetries == total attempt cap, not additional retries
// after the first call). The test asserts the counter == 3 and an error
// is returned.
func TestClientGivesUpAfterMaxRetries(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token",
		WithRetryBaseDelay(1*time.Millisecond),
		WithMaxRetries(3),
	)

	var out AdventureRequirements
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := client.Get(ctx, "/foo", "https://advancements.scouting.org/roster", &out)
	if err == nil {
		t.Fatalf("expected error after exhausting retries, got nil")
	}
	if got, want := count.Load(), int32(3); got != want {
		t.Errorf("request count = %d, want %d", got, want)
	}
}
