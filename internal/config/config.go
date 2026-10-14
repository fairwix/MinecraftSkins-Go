package config

import (
	"fmt"
	"os"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	CoinGeckoBaseURL string
	AdminAPIKey      string
	DemoUserAPIKey   string
	DemoUserID       string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:         env("HTTP_ADDR", ":8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		CoinGeckoBaseURL: env("COINGECKO_BASE_URL", "https://api.coingecko.com/api/v3/"),
		AdminAPIKey:      os.Getenv("ADMIN_API_KEY"),
		DemoUserAPIKey:   os.Getenv("DEMO_USER_API_KEY"),
		DemoUserID:       env("DEMO_USER_ID", "demo-user"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.AdminAPIKey) < 32 {
		return Config{}, fmt.Errorf("ADMIN_API_KEY must contain at least 32 characters")
	}
	if len(cfg.DemoUserAPIKey) < 32 {
		return Config{}, fmt.Errorf("DEMO_USER_API_KEY must contain at least 32 characters")
	}
	if cfg.AdminAPIKey == cfg.DemoUserAPIKey {
		return Config{}, fmt.Errorf("ADMIN_API_KEY and DEMO_USER_API_KEY must be different")
	}
	if cfg.DemoUserID == "" || len(cfg.DemoUserID) > 100 {
		return Config{}, fmt.Errorf("DEMO_USER_ID must contain between 1 and 100 characters")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
