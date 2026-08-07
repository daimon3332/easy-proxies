package subfetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchUsesConfiguredDirectRequestBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises the historical ten-second direct timeout")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10*time.Second + 100*time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	body, err := Fetch(ctx, server.URL, Options{Timeout: 12 * time.Second})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("Fetch() body = %q, want ok", body)
	}
}

func TestFetchDoesNotFallbackForPermanentHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()

	var fallbacks atomic.Int32
	_, err := Fetch(context.Background(), server.URL, Options{
		Timeout: time.Second,
		ProxyFallback: func(context.Context, string, http.Header) ([]byte, error) {
			fallbacks.Add(1)
			return nil, errors.New("unexpected fallback")
		},
	})
	if err == nil {
		t.Fatal("Fetch() succeeded for HTTP 401")
	}
	if got := fallbacks.Load(); got != 0 {
		t.Fatalf("proxy fallback calls = %d, want 0", got)
	}
}

func TestFetchRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxBodySize+1)))
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL, Options{Timeout: 5 * time.Second})
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("Fetch() error = %v, want ErrBodyTooLarge", err)
	}
}

func TestFetchRejectsOversizedFallbackBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "retry", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL, Options{
		Timeout: time.Second,
		ProxyFallback: func(context.Context, string, http.Header) ([]byte, error) {
			return []byte(strings.Repeat("x", maxBodySize+1)), nil
		},
	})
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("Fetch() fallback error = %v, want ErrBodyTooLarge", err)
	}
}

func TestFetchErrorDoesNotExposeSourceURL(t *testing.T) {
	rawURL := "http://127.0.0.1:1/sub?token=secret"
	_, err := Fetch(context.Background(), rawURL, Options{Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("Fetch() unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), rawURL) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Fetch() exposed source URL in error: %v", err)
	}
}
