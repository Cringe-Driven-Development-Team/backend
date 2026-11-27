// Package identity собирает отдельный gRPC-вход проверки пользовательских сессий.
package identity

import (
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// SessionConfig содержит только зависимости первого gRPC-входа Identity.
type SessionConfig struct {
	LogLevel                            slog.Level
	GRPCAddr, Version                   string
	JWTSecret, RedisAddr, RedisPassword string
	TLSCertFile, TLSKeyFile, TLSCAFile  string
	RedisDB                             int
	RedisTimeout                        time.Duration
}

// LoadSessionConfig читает настройки через lookup без зависимости от глобальной среды.
// Полный HTTP API Identity подключается отдельно; PostgreSQL/S3 ему понадобятся на том этапе.
func LoadSessionConfig(lookup func(string) (string, bool)) (SessionConfig, error) {
	get := func(name, fallback string) string {
		if value, ok := lookup(name); ok {
			return value
		}
		return fallback
	}
	c := SessionConfig{GRPCAddr: get("GRPC_ADDR", ":9090"), Version: get("VERSION", "local"), JWTSecret: get("JWT_SECRET", ""), RedisPassword: get("REDIS_PASSWORD", ""), TLSCertFile: get("TLS_CERT_FILE", ""), TLSKeyFile: get("TLS_KEY_FILE", ""), TLSCAFile: get("TLS_CA_FILE", "")}
	switch get("LOG_LEVEL", "info") {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		return SessionConfig{}, errors.New("invalid LOG_LEVEL")
	}
	for _, setting := range []struct{ name, value string }{{"JWT_SECRET", c.JWTSecret}, {"TLS_CERT_FILE", c.TLSCertFile}, {"TLS_KEY_FILE", c.TLSKeyFile}, {"TLS_CA_FILE", c.TLSCAFile}} {
		if strings.TrimSpace(setting.value) == "" {
			return SessionConfig{}, errors.New("missing " + setting.name)
		}
	}
	host := get("REDIS_HOST", "redis")
	port, err := strconv.Atoi(get("REDIS_PORT", "6379"))
	if err != nil || port < 1 || port > 65535 || strings.TrimSpace(host) == "" || strings.ContainsAny(host, " /\t\r\n") {
		return SessionConfig{}, errors.New("invalid Redis address")
	}
	c.RedisAddr = net.JoinHostPort(host, strconv.Itoa(port))
	c.RedisDB, err = strconv.Atoi(get("REDIS_DB", "0"))
	if err != nil || c.RedisDB < 0 {
		return SessionConfig{}, errors.New("invalid REDIS_DB")
	}
	c.RedisTimeout, err = time.ParseDuration(get("REDIS_OPERATION_TIMEOUT", "1s"))
	if err != nil || c.RedisTimeout <= 0 {
		return SessionConfig{}, errors.New("invalid REDIS_OPERATION_TIMEOUT")
	}
	if _, _, err = net.SplitHostPort(c.GRPCAddr); err != nil {
		return SessionConfig{}, errors.New("invalid GRPC_ADDR")
	}
	return c, nil
}
