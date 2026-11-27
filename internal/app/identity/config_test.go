package identity

import (
	"strings"
	"testing"
)

// TestLoadSessionConfig проверяет изоляцию от чужих секретов и запрет plaintext запуска.
func TestLoadSessionConfig(t *testing.T) {
	valid := map[string]string{"JWT_SECRET": "test-only-secret", "TLS_CERT_FILE": "identity.crt", "TLS_KEY_FILE": "identity.key", "TLS_CA_FILE": "ca.crt"}
	lookup := func(key string) (string, bool) { v, ok := valid[key]; return v, ok }
	cfg, err := LoadSessionConfig(lookup)
	if err != nil || cfg.GRPCAddr != ":9090" || cfg.RedisAddr != "redis:6379" || cfg.RedisDB != 0 {
		t.Fatalf("config=%+v, err=%v", cfg, err)
	}
	for _, key := range []string{"JWT_SECRET", "TLS_CERT_FILE", "TLS_KEY_FILE", "TLS_CA_FILE"} {
		t.Run(key, func(t *testing.T) {
			_, err := LoadSessionConfig(func(name string) (string, bool) {
				if name == key {
					return "", false
				}
				return lookup(name)
			})
			if err == nil || strings.Contains(err.Error(), "test-only-secret") {
				t.Fatalf("expected safe required-setting error, got %v", err)
			}
		})
	}
	for _, key := range []string{"REDIS_PORT", "REDIS_DB", "REDIS_OPERATION_TIMEOUT", "LOG_LEVEL"} {
		t.Run("invalid "+key, func(t *testing.T) {
			_, err := LoadSessionConfig(func(name string) (string, bool) {
				if name == key {
					return "-1", true
				}
				return lookup(name)
			})
			if err == nil {
				t.Fatal("accepted invalid Redis configuration")
			}
		})
	}
}

// TestLoadConfig проверяет независимость полного Identity от секретов Workspace/Environments.
func TestLoadConfig(t *testing.T) {
	values := map[string]string{"JWT_SECRET": "secret", "TLS_CERT_FILE": "identity.crt", "TLS_KEY_FILE": "identity.key", "TLS_CA_FILE": "ca.crt", "DATABASE_URL": "postgres://identity:secret@localhost/app", "BFF_API_KEY": "bff", "TOTP_ENCRYPTION_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "S3_AVATARS_BUCKET": "avatars", "S3_AVATARS_PUBLIC_URL": "https://cdn.example/avatars"}
	lookup := func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	c, err := LoadConfig(lookup)
	if err != nil || c.HTTPAddr != ":8080" || c.AdminAddr != ":9091" {
		t.Fatalf("full config: %v", err)
	}
	for _, key := range []string{"DATABASE_URL", "BFF_API_KEY", "TOTP_ENCRYPTION_KEY", "S3_AVATARS_BUCKET", "S3_AVATARS_PUBLIC_URL", "HTTP_TIMEOUT", "SHUTDOWN_TIMEOUT", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL", "HTTP_ADDR", "ADMIN_ADDR"} {
		t.Run(key, func(t *testing.T) {
			_, err := LoadConfig(func(name string) (string, bool) {
				if name == key {
					return "bad", true
				}
				return lookup(name)
			})
			if key == "BFF_API_KEY" || key == "S3_AVATARS_BUCKET" {
				_, err = LoadConfig(func(name string) (string, bool) {
					if name == key {
						return "", true
					}
					return lookup(name)
				})
			}
			if err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
}
