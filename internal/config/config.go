package config

import (
	"fmt"
	"os"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	CoinGeckoBaseURL string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:         env("HTTP_ADDR", ":8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		CoinGeckoBaseURL: env("COINGECKO_BASE_URL", "https://api.coingecko.com/api/v3/"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
