package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestHandlerAddsRequestIDOnlyFromContext(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewHandler(slog.NewJSONHandler(&out, nil)))
	logger.InfoContext(WithRequestID(context.Background(), "req-1"), "event")
	logger.InfoContext(context.Background(), "startup")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"request_id":"req-1"`) || strings.Contains(lines[1], `"request_id"`) {
		t.Fatalf("unexpected logs: %s", out.String())
	}
}

func TestHandlerPreservesAttrsAndGroups(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewHandler(slog.NewJSONHandler(&out, nil))).With("component", "api").WithGroup("request")
	logger.InfoContext(WithRequestID(context.Background(), "req-2"), "event")
	if !strings.Contains(out.String(), `"request_id":"req-2"`) || !strings.Contains(out.String(), `"component":"api"`) {
		t.Fatalf("lost attributes: %s", out.String())
	}
}

func TestErrorCodeHidesMessageAndPreservesCause(t *testing.T) {
	source := errors.New("SECRET")
	wrapped := WithCode(source, "startup_config")
	if ErrorCode(wrapped) != "startup_config" || !errors.Is(wrapped, source) || ErrorCode(source) != "internal_error" {
		t.Fatalf("wrong error classification: %q", ErrorCode(wrapped))
	}
}
