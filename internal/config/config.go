// Package config читает настройки API из окружения.
package config

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"time"
)

type Config struct {
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
		Host: get("HOST", "0.0.0.0"), Port: get("PORT", "8080"),
		WriteTimeout: duration("WRITE_TIMEOUT", 15*time.Second), ReadTimeout: duration("READ_TIMEOUT", 15*time.Second),
		JWTSecret: get("JWT_SECRET", ""), CSRFSecret: get("CSRF_SECRET", ""),
		AccessTTL: duration("ACCESS_TOKEN_TTL", 15*time.Minute), RefreshTTL: duration("REFRESH_TOKEN_TTL", 720*time.Hour),
		CookieSecure: get("COOKIE_SECURE", "false") == "true",
		CORSOrigins:  get("CORS_ALLOWED_ORIGINS", "https://cellestial.ru,http://localhost:5173"), AppOrigin: get("APP_ORIGIN", "https://cellestial.ru"),
		NotebooksBucket: get("S3_NOTEBOOKS_BUCKET", ""),
		DBUser:          get("POSTGRES_USER", "postgres"), DBPassword: get("POSTGRES_PASSWORD", "postgres"), DBHost: get("POSTGRES_HOST", "db"), DBName: get("POSTGRES_DB", "app_db"),
	}
	if c.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is not set")
	}
	if c.CSRFSecret == "" {
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
