// Package environments запускает внутренний gRPC API provisioning.
package environments

import (
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
)

// Config задаёт только сервисные ключи, локальную БД и провайдера Environments.
type Config struct {
	GRPCAddr, AdminAddr, DatabaseURL, Version string
	TLS, AgentTLS                             mtls.Config
	AgentPort                                 int
	Runtime                                   config.RuntimeConfig
	LogLevel                                  slog.Level
	ShutdownTimeout                           time.Duration
	WorkersPaused                             bool
}

// LoadConfig проверяет внутренние listeners и отдельную runtime PKI.
func LoadConfig(lookup func(string) (string, bool)) (Config, error) {
	runtime, err := config.LoadRuntimeFrom(lookup)
	if err != nil {
		return Config{}, err
	}
	get := func(name, fallback string) string {
		if value, ok := lookup(name); ok {
			return value
		}
		return fallback
	}
	c := Config{Runtime: runtime, GRPCAddr: get("GRPC_ADDR", ":9090"), AdminAddr: get("ADMIN_ADDR", ":9091"),
		DatabaseURL: get("DATABASE_URL", ""), Version: get("VERSION", "local"),
		TLS: mtls.Config{CAFile: get("TLS_CA_FILE", ""), CertFile: get("TLS_CERT_FILE", ""), KeyFile: get("TLS_KEY_FILE", "")},
	}
	c.AgentTLS = mtls.Config{CAFile: get("RUNTIME_TLS_CA_CERT_FILE", c.TLS.CAFile), CertFile: get("RUNTIME_TLS_CLIENT_CERT_FILE", c.TLS.CertFile), KeyFile: get("RUNTIME_TLS_CLIENT_KEY_FILE", c.TLS.KeyFile)}
	for _, setting := range []struct{ name, value string }{
		{"GRPC_ADDR", c.GRPCAddr}, {"ADMIN_ADDR", c.AdminAddr},
	} {
		_, port, addressErr := net.SplitHostPort(setting.value)
		n, portErr := strconv.Atoi(port)
		if addressErr != nil || portErr != nil || n < 0 || n > 65535 {
			return Config{}, errors.New("invalid " + setting.name)
		}
	}
	for _, setting := range []struct{ name, value string }{
		{"TLS_CA_FILE", c.TLS.CAFile}, {"TLS_CERT_FILE", c.TLS.CertFile}, {"TLS_KEY_FILE", c.TLS.KeyFile},
	} {
		if strings.TrimSpace(setting.value) == "" {
			return Config{}, errors.New("missing " + setting.name)
		}
	}
	db, err := url.Parse(c.DatabaseURL)
	if err != nil || db.Host == "" || (db.Scheme != "postgres" && db.Scheme != "postgresql") {
		return Config{}, errors.New("invalid DATABASE_URL")
	}
	c.AgentPort, err = strconv.Atoi(get("AGENT_GRPC_PORT", "9090"))
	if err != nil || c.AgentPort <= 0 || c.AgentPort > 65535 {
		return Config{}, errors.New("invalid AGENT_GRPC_PORT")
	}
	c.ShutdownTimeout, err = time.ParseDuration(get("SHUTDOWN_TIMEOUT", "30s"))
	if err != nil || c.ShutdownTimeout < time.Second {
		return Config{}, errors.New("invalid SHUTDOWN_TIMEOUT")
	}
	c.WorkersPaused, err = strconv.ParseBool(get("WORKERS_PAUSED", "false"))
	if err != nil {
		return Config{}, errors.New("invalid WORKERS_PAUSED")
	}
	if c.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))) != nil {
		return Config{}, errors.New("invalid LOG_LEVEL")
	}
	return c, nil
}
