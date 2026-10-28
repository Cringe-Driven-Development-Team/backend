package app

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

func TestStartupErrorLogsSafeCodeAndPreservesCause(t *testing.T) {
	for _, stage := range []string{"database_connect", "database_migrate", "s3_client", "runtime_init", "http_listen"} {
		source := errors.New("SECRET-credential")
		err := startupError(stage, source)
		if !errors.Is(err, source) || observability.ErrorCode(err) != stage {
			t.Fatalf("stage %s: code=%s", stage, observability.ErrorCode(err))
		}
		var out bytes.Buffer
		slog.New(observability.NewHandler(slog.NewJSONHandler(&out, nil))).Error("application stopped", "error_code", observability.ErrorCode(err))
		if strings.Contains(out.String(), "SECRET") || strings.Contains(out.String(), "request_id") {
			t.Fatalf("unsafe startup log: %s", out.String())
		}
	}
	if got := observability.ErrorCode(startupError("unknown", errors.New("SECRET"))); got != "internal_error" {
		t.Fatalf("unknown stage code=%s", got)
	}
}
