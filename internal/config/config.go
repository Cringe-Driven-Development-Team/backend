// Package config читает настройки API из окружения.
package config

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AuthMode                           string
	BFFS2SRequired                     bool
	BFFAPIKey, BFFAPIKeyPrevious       string
	LogLevel                           slog.Level
	Host, Port                         string
	WriteTimeout, ReadTimeout          time.Duration
	JWTSecret, CSRFSecret              string
	AccessTTL, RefreshTTL              time.Duration
	CookieSecure                       bool
	CORSOrigins, AppOrigin             string
	NotebooksBucket                    string
	DBUser, DBPassword, DBHost, DBName string
}

// Load читает окружение процесса.
func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom позволяет получить настройки без изменения окружения процесса.
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	duration := func(key string, fallback time.Duration) time.Duration {
		if value, ok := lookup(key); ok {
			if d, err := time.ParseDuration(value); err == nil {
				return d
			}
			slog.Warn("invalid duration in env, using fallback", slog.String("key", key))
		}
		return fallback
	}
	c := Config{
		AuthMode:  get("API_AUTH_MODE", "legacy"),
		BFFAPIKey: get("BFF_API_KEY", ""), BFFAPIKeyPrevious: get("BFF_API_KEY_PREVIOUS", ""),
		LogLevel: slog.LevelInfo,
		Host:     get("HOST", "0.0.0.0"), Port: get("PORT", "8080"),
		WriteTimeout: duration("WRITE_TIMEOUT", 15*time.Second), ReadTimeout: duration("READ_TIMEOUT", 15*time.Second),
		JWTSecret: get("JWT_SECRET", ""), CSRFSecret: get("CSRF_SECRET", ""),
		AccessTTL: duration("ACCESS_TOKEN_TTL", 15*time.Minute), RefreshTTL: duration("REFRESH_TOKEN_TTL", 720*time.Hour),
		CookieSecure: get("COOKIE_SECURE", "false") == "true",
		CORSOrigins:  get("CORS_ALLOWED_ORIGINS", "https://cellestial.ru,http://localhost:5173"), AppOrigin: get("APP_ORIGIN", "https://cellestial.ru"),
		NotebooksBucket: get("S3_NOTEBOOKS_BUCKET", ""),
		DBUser:          get("POSTGRES_USER", "postgres"), DBPassword: get("POSTGRES_PASSWORD", "postgres"), DBHost: get("POSTGRES_HOST", "db"), DBName: get("POSTGRES_DB", "app_db"),
	}
	if raw, ok := lookup("LOG_LEVEL"); ok {
		switch raw {
		case "debug":
			c.LogLevel = slog.LevelDebug
		case "info":
			c.LogLevel = slog.LevelInfo
		case "warn":
			c.LogLevel = slog.LevelWarn
		case "error":
			c.LogLevel = slog.LevelError
		default:
			return Config{}, errors.New("invalid LOG_LEVEL: expected debug, info, warn or error")
		}
	}
	if c.AuthMode != "legacy" && c.AuthMode != "bff" {
		return Config{}, errors.New("invalid API_AUTH_MODE: expected legacy or bff")
	}
	var err error
	c.BFFS2SRequired, err = strconv.ParseBool(get("BFF_S2S_REQUIRED", "false"))
	if err != nil {
		return Config{}, errors.New("invalid BFF_S2S_REQUIRED: expected boolean")
	}
	if c.BFFS2SRequired && (c.AuthMode != "bff" || c.BFFAPIKey == "") {
		return Config{}, errors.New("BFF_S2S_REQUIRED requires bff mode and BFF_API_KEY")
	}
	if c.BFFAPIKeyPrevious != "" && c.BFFAPIKey == "" {
		return Config{}, errors.New("BFF_API_KEY_PREVIOUS requires BFF_API_KEY")
	}
	if c.AccessTTL < time.Second || c.RefreshTTL < time.Second {
		return Config{}, errors.New("token TTL must be at least one second")
	}
	if c.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is not set")
	}
	if c.AuthMode == "legacy" && c.CSRFSecret == "" {
		return Config{}, errors.New("CSRF_SECRET is not set")
	}
	if c.NotebooksBucket == "" {
		return Config{}, errors.New("S3_NOTEBOOKS_BUCKET is not set")
	}
	return c, nil
}

// PostgresURL сохраняет специальные символы в имени и пароле.
func (c Config) PostgresURL() string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.DBUser, c.DBPassword), Host: c.DBHost + ":5432", Path: "/" + c.DBName}
	return u.String()
}
