package ratestore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/shopspring/decimal"
)

const (
	cacheTTL      = time.Minute
	fallbackLimit = 10 * time.Minute
)

type Store struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger

	mu         sync.Mutex
	fresh      *domain.Rate
	freshUntil time.Time
	fallback   *domain.Rate
	fetching   chan struct{}
}

func New(baseURL string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 5 * time.Second},
		logger:  logger,
	}
}

func (s *Store) Current(ctx context.Context) (domain.Rate, error) {
	for {
		s.mu.Lock()
		now := time.Now().UTC()
		if s.fresh != nil && now.Before(s.freshUntil) {
			result := withAge(*s.fresh, "Cache", now)
			s.mu.Unlock()
			return result, nil
		}
		if pending := s.fetching; pending != nil {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return domain.Rate{}, ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		s.fetching = pending
		s.mu.Unlock()

		value, err := s.fetch(ctx)
		s.mu.Lock()
		if err == nil {
			value.Source = "External"
			s.fresh = &value
			s.freshUntil = time.Now().UTC().Add(cacheTTL)
			s.fallback = &value
			s.logger.Info("fetched BTC/USD rate", "rate", value.Rate.String())
		}
		close(pending)
		s.fetching = nil
		fallback := s.fallback
		s.mu.Unlock()

		if err == nil {
			return value, nil
		}
		if ctx.Err() != nil {
			return domain.Rate{}, ctx.Err()
		}
		if fallback != nil && time.Since(fallback.AsOfUTC) <= fallbackLimit {
			s.logger.Warn("using cached BTC/USD fallback", "age", time.Since(fallback.AsOfUTC).Round(time.Second).String())
			return withAge(*fallback, "Fallback", time.Now().UTC()), nil
		}
		return domain.Rate{}, &domain.ExternalServiceError{Service: "CoinGecko", Cause: err}
	}
}

func (s *Store) fetch(ctx context.Context) (domain.Rate, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.baseURL+"/simple/price?ids=bitcoin&vs_currencies=usd", nil)
	if err != nil {
		return domain.Rate{}, fmt.Errorf("build CoinGecko request: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return domain.Rate{}, fmt.Errorf("request CoinGecko rate: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.Rate{}, fmt.Errorf("CoinGecko returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Bitcoin struct {
			USD json.Number `json:"usd"`
		} `json:"bitcoin"`
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return domain.Rate{}, fmt.Errorf("decode CoinGecko rate: %w", err)
	}
	rate, err := decimal.NewFromString(payload.Bitcoin.USD.String())
	if err != nil || !rate.GreaterThan(decimal.Zero) {
		return domain.Rate{}, fmt.Errorf("CoinGecko returned an invalid BTC/USD rate")
	}
	return domain.Rate{Rate: rate, AsOfUTC: time.Now().UTC()}, nil
}

func withAge(rate domain.Rate, source string, now time.Time) domain.Rate {
	age := int(now.Sub(rate.AsOfUTC).Seconds())
	if age < 0 {
		age = 0
	}
	rate.Source = source
	rate.AgeSeconds = &age
	return rate
}
