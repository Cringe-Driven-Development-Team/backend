// Package workspace собирает HTTP API блокнотов и workers пользовательских runtime-сессий.
package workspace

import (
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
)

// Config содержит только зависимости Workspace и адреса двух удалённых сервисов.
type Config struct {
	HTTPAddr, GRPCAddr, AdminAddr           string
	IdentityAddr, EnvironmentsAddr          string
	DatabaseURL, BFFKey, CursorKey, Version string
	TLS                                     mtls.Config
	LogLevel                                slog.Level
	HTTPTimeout, ShutdownTimeout            time.Duration
	WorkersPaused                           bool
}

// LoadConfig читает отдельный ключ подписи cursor и сертификат Workspace.
func LoadConfig(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	c := Config{
		HTTPAddr: get("HTTP_ADDR", ":8080"), GRPCAddr: get("GRPC_ADDR", ":9090"), AdminAddr: get("ADMIN_ADDR", ":9091"),
		IdentityAddr: get("IDENTITY_GRPC_ADDR", ""), EnvironmentsAddr: get("ENVIRONMENTS_GRPC_ADDR", ""),
		DatabaseURL: get("DATABASE_URL", ""), BFFKey: get("BFF_API_KEY", ""), CursorKey: get("WORKSPACE_CURSOR_KEY", ""), Version: get("VERSION", "local"),
		TLS: mtls.Config{CertFile: get("TLS_CERT_FILE", ""), KeyFile: get("TLS_KEY_FILE", ""), CAFile: get("TLS_CA_FILE", "")},
	}
	if c.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))) != nil {
		return Config{}, errors.New("invalid LOG_LEVEL")
	}
	for _, setting := range []struct{ name, value string }{
		{"HTTP_ADDR", c.HTTPAddr}, {"GRPC_ADDR", c.GRPCAddr}, {"ADMIN_ADDR", c.AdminAddr},
		{"IDENTITY_GRPC_ADDR", c.IdentityAddr}, {"ENVIRONMENTS_GRPC_ADDR", c.EnvironmentsAddr},
	} {
		host, port, err := net.SplitHostPort(setting.value)
		number, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || number < 0 || number > 65535 ||
			((setting.name == "IDENTITY_GRPC_ADDR" || setting.name == "ENVIRONMENTS_GRPC_ADDR") && (host == "" || number == 0)) {
			return Config{}, errors.New("invalid " + setting.name)
		}
	}
	for _, setting := range []struct{ name, value string }{
		{"BFF_API_KEY", c.BFFKey}, {"TLS_CERT_FILE", c.TLS.CertFile}, {"TLS_KEY_FILE", c.TLS.KeyFile}, {"TLS_CA_FILE", c.TLS.CAFile},
	} {
		if strings.TrimSpace(setting.value) == "" {
			return Config{}, errors.New("missing " + setting.name)
		}
	}
	if len(strings.TrimSpace(c.CursorKey)) < 32 {
		return Config{}, errors.New("WORKSPACE_CURSOR_KEY must contain at least 32 bytes")
	}
	db, err := url.Parse(c.DatabaseURL)
	if err != nil || (db.Scheme != "postgres" && db.Scheme != "postgresql") || db.Host == "" {
		return Config{}, errors.New("invalid DATABASE_URL")
	}
	c.WorkersPaused, err = strconv.ParseBool(get("WORKERS_PAUSED", "false"))
	if err != nil {
		return Config{}, errors.New("invalid WORKERS_PAUSED")
	}
	for _, setting := range []struct {
		name, fallback string
		target         *time.Duration
	}{
		{"HTTP_TIMEOUT", "30s", &c.HTTPTimeout}, {"SHUTDOWN_TIMEOUT", "30s", &c.ShutdownTimeout},
	} {
		value, err := time.ParseDuration(get(setting.name, setting.fallback))
		if err != nil || value < time.Second {
			return Config{}, errors.New("invalid " + setting.name)
		}
		*setting.target = value
	}
	return c, nil
}
