package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// requestMetaKey хранит уникальный ключ метаданных access log.
type requestMetaKey struct{}

// requestMeta хранит шаблон маршрута для сводки запроса.
type requestMeta struct {
	route          string
	responseFailed bool
}

// RequestID принимает безопасный X-Request-ID или создаёт новый и передаёт его в next.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if len(r.Header.Values("X-Request-ID")) != 1 || !observability.ValidRequestID(reqID) {
			reqID = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", reqID)

		ctx := observability.WithRequestID(r.Context(), reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logging записывает метод, шаблон маршрута, статус и длительность вызова next.
func Logging(next http.Handler) http.Handler {
	return Observe(next, nil)
}

// HTTPObserver получает только метод, известный шаблон маршрута, статус и длительность.
type HTTPObserver func(method, route string, status int, duration time.Duration)

// Observe добавляет наблюдение за тем же итогом запроса, который попадает в лог.
func Observe(next http.Handler, observe HTTPObserver) http.Handler {
	return ObserveHTTP(next, observe, nil, nil)
}

// ObserveHTTP измеряет итог запроса, восстанавливая шаблон при раннем отказе.
func ObserveHTTP(next http.Handler, observe HTTPObserver, metrics *HTTPMetrics, route func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		meta := &requestMeta{route: "unmatched"}
		ctx := context.WithValue(r.Context(), requestMetaKey{}, meta)
		writer := newResponseWriter(w)
		next.ServeHTTP(writer.withFlush(), r.WithContext(ctx))
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		if meta.route == "unmatched" && route != nil {
			meta.route = route(r)
		}
		method := observability.HTTPMethod(r.Method)
		duration := time.Since(start)
		metrics.observe(method, meta.route, status, duration, meta.responseFailed || writer.failed)
		if observe != nil {
			observe(method, meta.route, status, duration)
		}
		slog.InfoContext(ctx, "http.request",
			slog.String("method", method),
			slog.String("route", meta.route),
			slog.Int("status", status),
			slog.Duration("duration", duration),
		)
	})
}

// RecordRoute сохраняет шаблон сопоставленного маршрута для access log.
func RecordRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if meta, ok := r.Context().Value(requestMetaKey{}).(*requestMeta); ok {
			if route := mux.CurrentRoute(r); route != nil {
				if template, err := route.GetPathTemplate(); err == nil {
					meta.route = template
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Recover преобразует panic из next во внутреннюю HTTP-ошибку.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if writer, ok := w.(interface{ responseStarted() bool }); ok && writer.responseStarted() {
					MarkResponseFailure(r)
					slog.ErrorContext(r.Context(), "http.response.failed", slog.String("error_code", "panic"))
					return
				}
				httperr.Internal(w, r, fmt.Errorf("panic: %v", rec))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
