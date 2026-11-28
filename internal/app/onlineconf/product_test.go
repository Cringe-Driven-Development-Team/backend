package onlineconf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/productconfig"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type productSessions struct{}

// ValidateSession связывает тестовые токены с пользователями A и B.
func (productSessions) ValidateSession(_ context.Context, r *identityv1.ValidateSessionRequest, _ ...grpc.CallOption) (*identityv1.ValidateSessionResponse, error) {
	switch r.GetAccessToken() {
	case "a":
		return &identityv1.ValidateSessionResponse{UserId: 1}, nil
	case "b":
		return &identityv1.ValidateSessionResponse{UserId: 2}, nil
	default:
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}
}

// TestProductSettings проверяет оба сервисных входа, правила персонализации и свежесть.
func TestProductSettings(t *testing.T) {
	config := func(value string) SnapshotEntry {
		return SnapshotEntry{Type: "config", Data: map[string]json.RawMessage{"value": json.RawMessage(value)}}
	}
	flag := func(enabled, percent string) SnapshotEntry {
		return SnapshotEntry{Type: "flag", Data: map[string]json.RawMessage{
			"enabled": json.RawMessage(enabled), "rollout_percent": json.RawMessage(percent),
			"include_users": json.RawMessage(`[{"id":1}]`), "exclude_users": json.RawMessage(`[{"id":2}]`),
		}}
	}
	for _, tc := range []struct {
		name, token, header, key string
		view                     ProductView
		code                     int
		want                     string
	}{
		{"BFF include", "a", "X-BFF-Key", "bff", ProductView{map[string]SnapshotEntry{"bff.notebook_max_cells": config("3"), "workspace.notebook_run_all": flag("true", "0")}}, 200, `{"notebook_max_cells":3,"notebook_run_all":true}`},
		{"Workspace include", "a", "X-Workspace-Key", "workspace", ProductView{map[string]SnapshotEntry{"bff.notebook_max_cells": config("3"), "workspace.notebook_run_all": flag("true", "0")}}, 200, `{"notebook_max_cells":3,"notebook_run_all":true}`},
		{"outside rollout", "b", "X-BFF-Key", "bff", ProductView{map[string]SnapshotEntry{"workspace.notebook_run_all": flag("true", "0")}}, 200, `{"notebook_max_cells":1000,"notebook_run_all":false}`},
		{"excluded at 100 percent", "b", "X-Workspace-Key", "workspace", ProductView{map[string]SnapshotEntry{"bff.notebook_max_cells": config("1001"), "workspace.notebook_run_all": flag("true", "100")}}, 200, `{"notebook_max_cells":1000,"notebook_run_all":false}`},
		{"disabled for included user", "a", "X-BFF-Key", "bff", ProductView{map[string]SnapshotEntry{"bff.notebook_max_cells": config(`"3"`), "workspace.notebook_run_all": flag("false", "100")}}, 200, `{"notebook_max_cells":1000,"notebook_run_all":false}`},
		{"missing keys", "a", "X-BFF-Key", "bff", ProductView{map[string]SnapshotEntry{}}, 200, `{"notebook_max_cells":1000,"notebook_run_all":true}`},
		{"snapshot unavailable", "a", "X-Workspace-Key", "workspace", ProductView{}, 503, `{"code":"onlineconf_unavailable","message":"OnlineConf unavailable"}`},
		{"wrong service key", "a", "X-Workspace-Key", "bff", ProductView{}, 403, `{"code":"s2s_forbidden","message":"service access forbidden"}`},
		{"invalid session", "revoked", "X-BFF-Key", "bff", ProductView{}, 401, `{"code":"unauthorized","message":"authentication required"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			product := productHandler(productSessions{}, func(context.Context) (ProductView, error) { return tc.view, nil }, "bff", "workspace")
			router := newRouter(nil, http.NewServeMux(), "bff", product)
			req := httptest.NewRequest(http.MethodGet, productconfig.Path+"?user_id=1", nil)
			req.Header.Set(tc.header, tc.key)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if tc.code != 200 {
				tc.want = tc.want[:len(tc.want)-1] + `,"request_id":"` + w.Header().Get("X-Request-ID") + `"}`
			}
			if w.Code != tc.code || w.Body.String() != tc.want+"\n" {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestProductSettingsRedis проверяет изменения через админку, PostgreSQL и Redis.
func TestProductSettingsRedis(t *testing.T) {
	cache := testRedisSnapshots(t)
	pool, err := pgxpool.New(t.Context(), onlineConfTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migration.Up(t.Context(), pool, migration.OnlineConf); err != nil {
		t.Fatal(err)
	}
	store := keyStore{pool: pool}
	refresher := newSnapshotRefresher(store, cache, testSnapshotLogger())
	product := productHandler(productSessions{}, cache.ProductView, "bff", "workspace")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	defer cancel()
	router := newRouter(testAdmin{}, newKeysHandler(store, &testDirectory{}, refresher.RequestRefresh), "bff", product)
	request := func(method, path, token, header, key, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(header, key)
		router.ServeHTTP(w, req)
		return w
	}
	settings := func() *httptest.ResponseRecorder {
		return request(http.MethodGet, productconfig.Path, "a", productconfig.WorkspaceKeyHeader, "workspace", "")
	}
	check := func(method, path, body string, want int) {
		t.Helper()
		w := request(method, "/admin/keys"+path, "test", "X-BFF-Key", "bff", body)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
	}
	await := func(body string) {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for {
			w := settings()
			if w.Code == 200 && strings.Contains(w.Body.String(), body) {
				return
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("expected %s, got %d %s", body, w.Code, w.Body.String())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if w := settings(); w.Code != 503 {
		t.Fatalf("empty snapshot: %d %s", w.Code, w.Body.String())
	}
	go func() { defer close(done); _ = refresher.Run(ctx) }()
	defer func() { cancel(); <-done }()
	await(`"notebook_max_cells":1000`)
	const key = "/bff.notebook_max_cells"
	check(http.MethodPost, "", `{"name":"bff.notebook_max_cells","scope":"bff","type":"config","format":"json"}`, 201)
	for _, value := range []string{"3", "4"} {
		check(http.MethodPut, key, `{"description":"","value":`+value+`}`, 200)
		await(`"notebook_max_cells":` + value)
	}
	check(http.MethodDelete, key, "", 204)
	await(`"notebook_max_cells":1000`)
	cancel()
	<-done
	if err := cache.client.Del(t.Context(), cache.prefix+":snapshot").Err(); err != nil {
		t.Fatal(err)
	}
	if w := settings(); w.Code != 503 {
		t.Fatalf("missing snapshot: %d %s", w.Code, w.Body.String())
	}
}
