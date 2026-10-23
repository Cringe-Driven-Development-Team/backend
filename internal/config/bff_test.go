package config

import (
	"strings"
	"testing"
)

func TestBFFConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		extra   map[string]string
		wantErr bool
	}{
		{"legacy default", nil, false},
		{"bff without csrf", map[string]string{"API_AUTH_MODE": "bff", "CSRF_SECRET": ""}, false},
		{"required without key", map[string]string{"API_AUTH_MODE": "bff", "BFF_S2S_REQUIRED": "true"}, true},
		{"rotation", map[string]string{"API_AUTH_MODE": "bff", "BFF_S2S_REQUIRED": "true", "BFF_API_KEY": "current-secret", "BFF_API_KEY_PREVIOUS": "previous-secret"}, false},
		{"previous alone", map[string]string{"API_AUTH_MODE": "bff", "BFF_API_KEY_PREVIOUS": "previous-secret"}, true},
		{"required legacy", map[string]string{"BFF_S2S_REQUIRED": "true", "BFF_API_KEY": "current-secret"}, true},
		{"unknown mode", map[string]string{"API_AUTH_MODE": "invalid-secret"}, true},
		{"invalid bool", map[string]string{"BFF_S2S_REQUIRED": "invalid-secret"}, true},
		{"invalid ttl", map[string]string{"API_AUTH_MODE": "bff", "ACCESS_TOKEN_TTL": "0s"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"JWT_SECRET": "jwt", "CSRF_SECRET": "csrf", "S3_NOTEBOOKS_BUCKET": "bucket"}
			for k, v := range tc.extra {
				values[k] = v
			}
			c, err := LoadFrom(settings(values))
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected config error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("configuration error disclosed value")
			}
			if err == nil && tc.name == "legacy default" && c.AuthMode != "legacy" {
				t.Fatal("legacy must remain default")
			}
		})
	}
}
