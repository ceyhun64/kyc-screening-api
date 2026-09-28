// Package config reads settings from environment variables.
package config

import (
	"errors"
	"os"
)

// Config holds the service settings.
type Config struct {
	Port        string
	DatabaseURL string
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
