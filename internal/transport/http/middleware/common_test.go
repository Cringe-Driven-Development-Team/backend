package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/google/uuid"
)

func TestRequestIDRejectsUnsafeHeader(t *testing.T) {
	for _, incoming := range []string{"bad value", "bad\nvalue", strings.Repeat("x", 65)} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", incoming)
		w := httptest.NewRecorder()
		RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := observability.RequestID(r.Context()); got != w.Header().Get("X-Request-ID") {
				t.Fatalf("context ID %q differs from response", got)
			}
		})).ServeHTTP(w, req)
		if _, err := uuid.Parse(w.Header().Get("X-Request-ID")); err != nil {
			t.Fatalf("unsafe ID %q remained", incoming)
		}
	}
}

func TestLoggingRecordsSafeStatusOnce(t *testing.T) {
	var out bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, nil))))
	t.Cleanup(func() { slog.SetDefault(old) })
	handler := RequestID(Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})))
	req := httptest.NewRequest(http.MethodPost, "/secret/123?token=SECRET", nil)
	q := httptest.NewRecorder()
	handler.ServeHTTP(q, req)
	log := out.String()
	if strings.Count(log, `"msg":"http.request"`) != 1 || !strings.Contains(log, `"status":201`) || !strings.Contains(log, `"route":"unmatched"`) || !strings.Contains(log, q.Header().Get("X-Request-ID")) || strings.Contains(log, "SECRET") || strings.Contains(log, "/secret/123") {
		t.Fatalf("unexpected HTTP log: %s", log)
	}
}

func TestRecoveredPanicDoesNotLeakValue(t *testing.T) {
	var out bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, nil))))
	t.Cleanup(func() { slog.SetDefault(old) })
	handler := RequestID(Logging(Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("SECRET-panic")
	}))))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/secret/SECRET", nil))
	log := out.String()
	if w.Code != 500 || strings.Contains(log, "SECRET") || strings.Count(log, `"msg":"http.request"`) != 1 || strings.Count(log, w.Header().Get("X-Request-ID")) != 2 {
		t.Fatalf("unsafe panic handling: status=%d log=%s", w.Code, log)
	}
}
