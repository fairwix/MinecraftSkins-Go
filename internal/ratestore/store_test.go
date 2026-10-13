package ratestore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/shopspring/decimal"
)

func TestCurrentFetchesAndCachesRate(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"bitcoin":{"usd":65432.10}}`))
	}))
	defer server.Close()
	store := New(server.URL, slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	first, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != "External" || first.Rate.String() != "65432.1" || first.AgeSeconds != nil {
		t.Fatalf("unexpected first result: %+v", first)
	}
	second, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Source != "Cache" || second.AgeSeconds == nil || calls.Load() != 1 {
		t.Fatalf("cache not used: result=%+v calls=%d", second, calls.Load())
	}
}

func TestCurrentUsesFallbackWhenExternalServiceFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	store := New(server.URL, slog.New(slog.NewTextHandler(discardWriter{}, nil)))
	store.fallback = &domain.Rate{Rate: decimal.NewFromInt(50000), AsOfUTC: time.Now().Add(-time.Minute), Source: "External"}

	result, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "Fallback" || result.Rate.String() != "50000" || result.AgeSeconds == nil {
		t.Fatalf("unexpected fallback result: %+v", result)
	}
}

func TestCurrentReturnsExternalErrorWithoutFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	store := New(server.URL, slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	_, err := store.Current(context.Background())
	if !errors.Is(err, domain.ErrExternalService) {
		t.Fatalf("Current() error = %v, want external service error", err)
	}
}

func TestCurrentCollapsesConcurrentCacheMisses(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-started
		time.Sleep(20 * time.Millisecond)
		_, _ = fmt.Fprint(w, `{"bitcoin":{"usd":65000}`+`}`)
	}))
	defer server.Close()
	store := New(server.URL, slog.New(slog.NewTextHandler(discardWriter{}, nil)))
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Current(context.Background()); err != nil {
				t.Errorf("Current(): %v", err)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
