package config

import (
	"fmt"
	"os"
)

type Config struct {
	Env             string
	Addr            string
	DBPath          string
	BaseURL         string
	SuperAdminEmail string
	ResendAPIKey    string
	EmailFrom       string
}

func Load() (Config, error) {
	cfg := Config{
		Env:             getenv("PINGIT_ENV", "dev"),
		Addr:            getenv("PINGIT_ADDR", ":8080"),
		DBPath:          getenv("PINGIT_DB", "./pingit-dev.db"),
		BaseURL:         getenv("PINGIT_BASE_URL", "http://localhost:8080"),
		SuperAdminEmail: os.Getenv("PINGIT_SUPERADMIN_EMAIL"),
		ResendAPIKey:    os.Getenv("PINGIT_RESEND_API_KEY"),
		EmailFrom:       getenv("PINGIT_EMAIL_FROM", "pingit@example.com"),
	}

	switch cfg.Env {
	case "dev", "prod":
	default:
		return Config{}, fmt.Errorf("invalid PINGIT_ENV: %s", cfg.Env)
	}

	return cfg, nil
}

func (c Config) IsProd() bool {
	return c.Env == "prod"
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
