// Package config reads all server configuration from environment variables.
//
// There is no config file by design: env-only config plays well with systemd
// EnvironmentFile, docker compose `env_file`, and 12-factor expectations.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the resolved runtime configuration.
type Config struct {
	Addr        string
	BaseURL     string
	PGDSN       string
	RedisAddr   string
	RedisDB     int
	RateIP      int
	RateWindow  time.Duration
	MaxBody     int
	MaxMocks    int
	DefaultTTL  time.Duration
	MaxTTL      time.Duration
	DefaultLang string
	SSEMaxConns int
	SSEMaxPerIP int

	SpamPatternsFile string
	SpamAllowIPs     []string
}

// Load reads configuration from the environment.
//
// It returns an error if a required variable is missing or any value cannot
// be parsed. Defaults match .env.example and the documentation in
// ARCHITECTURE.md — keep the three in sync.
func Load() (Config, error) {
	c := Config{
		Addr:        getEnv("QUICKMOCK_ADDR", ":8080"),
		BaseURL:     getEnv("QUICKMOCK_BASE_URL", "http://localhost:8080"),
		PGDSN:       os.Getenv("QUICKMOCK_PG_DSN"),
		RedisAddr:   getEnv("QUICKMOCK_REDIS_ADDR", "127.0.0.1:6379"),
		DefaultLang: getEnv("QUICKMOCK_DEFAULT_LANG", "en"),
	}

	if c.PGDSN == "" {
		return c, fmt.Errorf("QUICKMOCK_PG_DSN is required")
	}

	var err error
	if c.RedisDB, err = getInt("QUICKMOCK_REDIS_DB", 0); err != nil {
		return c, err
	}
	if c.RateIP, err = getInt("QUICKMOCK_RATE_IP", 5000); err != nil {
		return c, err
	}
	if c.RateWindow, err = getDuration("QUICKMOCK_RATE_WINDOW", 8*time.Hour); err != nil {
		return c, err
	}
	if c.MaxBody, err = getInt("QUICKMOCK_MAX_BODY", 524288); err != nil {
		return c, err
	}
	if c.MaxMocks, err = getInt("QUICKMOCK_MAX_MOCKS", 50); err != nil {
		return c, err
	}
	if c.DefaultTTL, err = getDuration("QUICKMOCK_DEFAULT_TTL", 168*time.Hour); err != nil {
		return c, err
	}
	if c.MaxTTL, err = getDuration("QUICKMOCK_MAX_TTL", 720*time.Hour); err != nil {
		return c, err
	}
	if c.SSEMaxConns, err = getInt("QUICKMOCK_SSE_MAX_CONNS", 500); err != nil {
		return c, err
	}
	if c.SSEMaxPerIP, err = getInt("QUICKMOCK_SSE_MAX_PER_IP", 4); err != nil {
		return c, err
	}

	c.SpamPatternsFile = os.Getenv("QUICKMOCK_SPAM_PATTERNS_FILE")
	if v := os.Getenv("QUICKMOCK_SPAM_ALLOW_IPS"); v != "" {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				c.SpamAllowIPs = append(c.SpamAllowIPs, s)
			}
		}
	}

	return c, nil
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getInt(k string, def int) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid int for %s: %w", k, err)
	}
	return n, nil
}

func getDuration(k string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid duration for %s: %w", k, err)
	}
	return d, nil
}
