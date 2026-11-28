// Package productconfig описывает продуктовый контракт OnlineConf и его HTTP-клиент.
package productconfig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

// Path задаёт адрес продуктового метода OnlineConf.
const Path = "/api/v1/onlineconf/me"

// WorkspaceKeyHeader идентифицирует Workspace отдельным сервисным ключом.
const WorkspaceKeyHeader = "X-Workspace-Key"

// ErrUnavailable означает, что решение OnlineConf недоступно.
var ErrUnavailable = errors.New("OnlineConf unavailable")

// Settings содержит публичные настройки подтверждённой сессии.
type Settings struct {
	MaxCells int  `json:"notebook_max_cells"`
	RunAll   bool `json:"notebook_run_all"`
}

type bearerKey struct{}

// WithBearer сохраняет проверенный токен для запроса настроек той же сессии.
func WithBearer(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, bearerKey{}, token)
}

// NewReader создаёт клиент для чтения настроек в контексте текущей сессии.
// baseURL задаёт внутренний адрес OnlineConf, key идентифицирует Workspace.
// metrics измеряет HTTP-вызовы зависимости.
func NewReader(baseURL, key string, metrics *observability.HTTPClientMetrics) func(context.Context) (Settings, error) {
	client := &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return func(ctx context.Context) (Settings, error) {
		token, _ := ctx.Value(bearerKey{}).(string)
		if token == "" || key == "" || baseURL == "" {
			return Settings{}, ErrUnavailable
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+Path, nil)
		if err != nil {
			return Settings{}, ErrUnavailable
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(WorkspaceKeyHeader, key)
		if id := observability.RequestID(ctx); id != "" {
			req.Header.Set("X-Request-ID", id)
		}
		resp, err := metrics.Measure("onlineconf", func(*http.Request) string { return "get_settings" }, client.Do, req)
		if err != nil {
			return Settings{}, ErrUnavailable
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return Settings{}, ErrUnavailable
		}
		var result struct {
			MaxCells *int  `json:"notebook_max_cells"`
			RunAll   *bool `json:"notebook_run_all"`
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
		if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &result) != nil || result.MaxCells == nil || result.RunAll == nil {
			return Settings{}, ErrUnavailable
		}
		return Settings{MaxCells: *result.MaxCells, RunAll: *result.RunAll}, nil
	}
}
