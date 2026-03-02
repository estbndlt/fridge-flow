package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppName           string
	AppBaseURL        string
	ListenAddr        string
	DatabaseURL       string
	MigrationsDir     string
	TemplateDir       string
	StaticDir         string
	CookieSecure      bool
	TrustProxyHeaders bool
	SessionTTL        time.Duration
	PollInterval      time.Duration

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
}

func Load() (Config, error) {
	port := strings.TrimSpace(getenv("APP_PORT", "8080"))
	if _, err := strconv.Atoi(port); err != nil {
		return Config{}, fmt.Errorf("invalid APP_PORT: %w", err)
	}

	cfg := Config{
		AppName:            strings.TrimSpace(getenv("APP_NAME", "FridgeFlow")),
		AppBaseURL:         strings.TrimRight(strings.TrimSpace(getenv("APP_BASE_URL", "http://localhost:3000")), "/"),
		ListenAddr:         strings.TrimSpace(getenv("APP_LISTEN_ADDR", ":"+port)),
		DatabaseURL:        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		MigrationsDir:      strings.TrimSpace(getenv("MIGRATIONS_DIR", "internal/db/migrations")),
		TemplateDir:        strings.TrimSpace(getenv("TEMPLATE_DIR", "web/templates")),
		StaticDir:          strings.TrimSpace(getenv("STATIC_DIR", "web/static")),
		CookieSecure:       getenvBool("COOKIE_SECURE", false),
		TrustProxyHeaders:  getenvBool("TRUST_PROXY_HEADERS", false),
		SessionTTL:         time.Duration(getenvInt("SESSION_TTL_HOURS", 24*30)) * time.Hour,
		PollInterval:       time.Duration(getenvInt("POLL_INTERVAL_SECONDS", 10)) * time.Second,
		GoogleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		GoogleClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),
		GoogleRedirectURL:  strings.TrimSpace(os.Getenv("GOOGLE_REDIRECT_URL")),
	}

	switch {
	case cfg.DatabaseURL == "":
		return Config{}, errors.New("DATABASE_URL is required")
	case cfg.GoogleClientID == "":
		return Config{}, errors.New("GOOGLE_CLIENT_ID is required")
	case cfg.GoogleClientSecret == "":
		return Config{}, errors.New("GOOGLE_CLIENT_SECRET is required")
	case cfg.GoogleRedirectURL == "":
		return Config{}, errors.New("GOOGLE_REDIRECT_URL is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
