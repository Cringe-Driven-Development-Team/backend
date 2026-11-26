package identity

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config содержит только зависимости Identity; DATABASE_URL принадлежит ограниченной app-роли.
type Config struct {
	SessionConfig
	HTTPAddr, AdminAddr, WorkspaceAddr                  string
	DatabaseURL, BFFAPIKey                              string
	AvatarsBucket, AvatarsPublicURL                     string
	TOTPEncryptionKey                                   []byte
	AccessTTL, RefreshTTL, HTTPTimeout, ShutdownTimeout time.Duration
	WorkersPaused                                       bool
}

// LoadConfig валидирует настройки без чтения секретов других сервисов.
func LoadConfig(lookup func(string) (string, bool)) (Config, error) {
	session, err := LoadSessionConfig(lookup)
	if err != nil {
		return Config{}, err
	}
	get := func(name, fallback string) string {
		if v, ok := lookup(name); ok {
			return v
		}
		return fallback
	}
	c := Config{SessionConfig: session, HTTPAddr: get("HTTP_ADDR", ":8080"), AdminAddr: get("ADMIN_ADDR", ":9091"), WorkspaceAddr: get("WORKSPACE_GRPC_ADDR", "workspace:9090"), DatabaseURL: get("DATABASE_URL", ""), BFFAPIKey: get("BFF_API_KEY", ""), AvatarsBucket: get("S3_AVATARS_BUCKET", ""), AvatarsPublicURL: strings.TrimRight(get("S3_AVATARS_PUBLIC_URL", ""), "/")}
	for _, s := range []struct{ name, value string }{{"HTTP_ADDR", c.HTTPAddr}, {"ADMIN_ADDR", c.AdminAddr}, {"WORKSPACE_GRPC_ADDR", c.WorkspaceAddr}} {
		if _, _, addressErr := net.SplitHostPort(s.value); addressErr != nil {
			return Config{}, errors.New("invalid " + s.name)
		}
	}
	db, e := url.Parse(c.DatabaseURL)
	if e != nil || (db.Scheme != "postgres" && db.Scheme != "postgresql") || db.Host == "" {
		return Config{}, errors.New("invalid DATABASE_URL")
	}
	c.WorkersPaused, err = strconv.ParseBool(get("WORKERS_PAUSED", "false"))
	if err != nil {
		return Config{}, errors.New("invalid WORKERS_PAUSED")
	}
	for _, s := range []struct{ name, value string }{{"BFF_API_KEY", c.BFFAPIKey}, {"S3_AVATARS_BUCKET", c.AvatarsBucket}} {
		if strings.TrimSpace(s.value) == "" {
			return Config{}, errors.New("missing " + s.name)
		}
	}
	c.TOTPEncryptionKey, err = base64.StdEncoding.DecodeString(get("TOTP_ENCRYPTION_KEY", ""))
	if err != nil || len(c.TOTPEncryptionKey) != 32 {
		return Config{}, errors.New("invalid TOTP_ENCRYPTION_KEY")
	}
	u, e := url.Parse(c.AvatarsPublicURL)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(c.AvatarsPublicURL, "#") {
		return Config{}, errors.New("invalid S3_AVATARS_PUBLIC_URL")
	}
	for _, s := range []struct {
		name, def string
		target    *time.Duration
	}{{"ACCESS_TOKEN_TTL", "15m", &c.AccessTTL}, {"REFRESH_TOKEN_TTL", "720h", &c.RefreshTTL}, {"HTTP_TIMEOUT", "15s", &c.HTTPTimeout}, {"SHUTDOWN_TIMEOUT", "30s", &c.ShutdownTimeout}} {
		v, e := time.ParseDuration(get(s.name, s.def))
		if e != nil || v < time.Second {
			return Config{}, errors.New("invalid " + s.name)
		}
		*s.target = v
	}
	return c, nil
}
