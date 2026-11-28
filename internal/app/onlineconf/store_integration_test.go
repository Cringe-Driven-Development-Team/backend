package onlineconf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testDirectory возвращает небольшой каталог с известными и неизвестными ID.
type testDirectory struct {
	identityv1.UsersClient
	unavailable bool
}

// BatchGetUsers возвращает существующих пользователей из запроса.
func (d *testDirectory) BatchGetUsers(_ context.Context, r *identityv1.BatchGetUsersRequest, _ ...grpc.CallOption) (*identityv1.UsersResponse, error) {
	if d.unavailable {
		return nil, status.Error(codes.Unavailable, "test")
	}
	out := &identityv1.UsersResponse{}
	for _, id := range r.GetUserIds() {
		if id == 42 || id == 17 {
			out.Users = append(out.Users, &identityv1.User{Id: id, Login: fmt.Sprintf("user%d", id)})
		}
	}
	return out, nil
}

// BatchResolveUsers ищет известный логин без учёта регистра.
func (d *testDirectory) BatchResolveUsers(_ context.Context, r *identityv1.BatchResolveUsersRequest, _ ...grpc.CallOption) (*identityv1.UsersResponse, error) {
	if strings.EqualFold(r.GetLogins()[0], "user17") {
		return &identityv1.UsersResponse{Users: []*identityv1.User{{Id: 17, Login: "user17"}}}, nil
	}
	return &identityv1.UsersResponse{}, nil
}

// onlineConfTestDatabase изолирует проверку в отдельной временной базе.
func onlineConfTestDatabase(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SERVICE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SERVICE_TEST_DATABASE_URL to run PostgreSQL tests")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("onlineconf_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	parsed.Path = "/" + name
	return parsed.String()
}

// TestKeysPostgres проверяет контракт за барьером доступа и сохранность при новом подключении.
func TestKeysPostgres(t *testing.T) {
	ctx := t.Context()
	dsn := onlineConfTestDatabase(t)
	open := func() *pgxpool.Pool {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	pool := open()
	if err := migration.Up(ctx, pool, migration.OnlineConf); err != nil {
		t.Fatal(err)
	}
	directory := &testDirectory{}
	var refreshes atomic.Int32
	handler := newRouter(testAdmin{}, newKeysHandler(keyStore{pool}, directory, func() { refreshes.Add(1) }), "test")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/admin/"+path, strings.NewReader(body))
		r.Header.Set("X-BFF-Key", "test")
		r.Header.Set("Authorization", "Bearer test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	check := func(method, path, body string, want int) string {
		t.Helper()
		response := request(method, path, body)
		if response.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, response.Code, want, response.Body.String())
		}
		return response.Body.String()
	}
	const config = "keys/frontend.calendar"
	const flag = "keys/workspace.calendar"
	const configCreate = `{"name":"frontend.calendar","scope":"frontend","type":"config","format":"json"}`
	var pending sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		pending.Go(func() { results <- request("POST", "keys", configCreate).Code })
	}
	pending.Wait()
	first, second := <-results, <-results
	if first+second != 201+409 || (first != 201 && first != 409) {
		t.Fatalf("concurrent status %d, %d", first, second)
	}
	check("POST", "keys", `{"name":"workspace.calendar","scope":"workspace","type":"flag"}`, 201)
	for _, value := range []string{`{"a":1}`, `[false]`, `""`, `9007199254740993`, `0`, `false`, `null`} {
		check("PUT", config, `{"description":"","value":`+value+`}`, 200)
		var card map[string]json.RawMessage
		if err := json.Unmarshal([]byte(check("GET", config, "", 200)), &card); err != nil || string(card["value"]) != value {
			t.Fatalf("round-trip %s: got %s, err %v", value, card["value"], err)
		}
	}
	check("PUT", config, `{"description":"invalid","value":}`, 400)
	check("PUT", flag, `{"description":"jump","enabled":true,"rollout_percent":25,"include_user_ids":[],"exclude_user_ids":[]}`, 409)
	check("PUT", flag, `{"description":"on","enabled":true,"rollout_percent":10,"include_user_ids":[42],"exclude_user_ids":[17]}`, 200)
	on := check("GET", flag, "", 200)
	refreshCount := refreshes.Load()
	rejected := check("PUT", flag, `{"description":"early","enabled":true,"rollout_percent":25,"include_user_ids":[],"exclude_user_ids":[]}`, 409)
	if !strings.Contains(rejected, `"code":"rollout_step_not_allowed"`) ||
		check("GET", flag, "", 200) != on || refreshes.Load() != refreshCount {
		t.Fatalf("rollout conflict changed state or refresh count: %s", rejected)
	}
	if _, err := pool.Exec(ctx, `UPDATE onlineconf.keys SET rollout_started_at = clock_timestamp() - interval '60 seconds' WHERE name = 'workspace.calendar'`); err != nil {
		t.Fatal(err)
	}
	steps := make(chan int, 2)
	for _, percent := range []int{25, 50} {
		pending.Go(func() {
			steps <- request("PUT", flag, fmt.Sprintf(`{"description":"on","enabled":true,"rollout_percent":%d,"include_user_ids":[42],"exclude_user_ids":[17]}`, percent)).Code
		})
	}
	pending.Wait()
	first, second = <-steps, <-steps
	if first+second != 200+409 || (first != 200 && first != 409) || refreshes.Load() != refreshCount+1 {
		t.Fatalf("concurrent rollout status %d, %d", first, second)
	}
	check("PUT", flag, `{"description":"lower","enabled":true,"rollout_percent":10,"include_user_ids":[42],"exclude_user_ids":[17]}`, 200)
	before := check("PUT", flag, `{"description":"off","enabled":false,"rollout_percent":10,"include_user_ids":[42],"exclude_user_ids":[17]}`, 200)
	for _, body := range []string{
		`{"description":"bad","enabled":true,"rollout_percent":101,"include_user_ids":[],"exclude_user_ids":[]}`,
		`{"description":"bad","enabled":true,"rollout_percent":100,"include_user_ids":[42],"exclude_user_ids":[42]}`,
		`{"description":"bad","enabled":true,"rollout_percent":0,"include_user_ids":[23],"exclude_user_ids":[]}`,
		`{"description":"bad","value":false}`,
	} {
		check("PUT", flag, body, 400)
		if after := check("GET", flag, "", 200); after != before {
			t.Fatalf("rejected update changed card: %s", after)
		}
	}
	for _, query := range []string{"?search=%20CaLeNdAr%20&scope=workspace&type=flag", "?scope=identity", ""} {
		var list struct {
			Items      []map[string]json.RawMessage `json:"items"`
			Total      int                          `json:"total_count"`
			ScopeCount int                          `json:"scope_count"`
			Scopes     []string                     `json:"scopes"`
		}
		if err := json.Unmarshal([]byte(check("GET", "keys"+query, "", 200)), &list); err != nil {
			t.Fatal(err)
		}
		if list.Total != 2 || list.ScopeCount != 2 || len(list.Scopes) != 4 || list.Items == nil {
			t.Fatalf("list: %+v", list)
		}
		switch query {
		case "":
			if len(list.Items) != 2 || string(list.Items[0]["name"]) != `"workspace.calendar"` {
				t.Fatalf("list order: %+v", list.Items)
			}
		case "?scope=identity":
			if len(list.Items) != 0 {
				t.Fatalf("empty scope: %+v", list.Items)
			}
		default:
			if len(list.Items) != 1 || string(list.Items[0]["name"]) != `"workspace.calendar"` || string(list.Items[0]["rollout_percent"]) != "10" {
				t.Fatalf("filtered items: %+v", list.Items)
			}
		}
	}
	check("GET", "users/resolve?id=17", "", 200)
	check("GET", "users/resolve?login=USER17", "", 200)
	check("GET", "users/resolve?id=17&login=user17", "", 400)
	check("GET", "users/resolve?id=23", "", 404)
	directory.unavailable = true
	check("GET", "users/resolve?id=17", "", 503)
	directory.unavailable = false
	pool.Close()
	handler = newRouter(testAdmin{}, newKeysHandler(keyStore{open()}, directory, nil), "test")
	if after := check("GET", flag, "", 200); after != before {
		t.Fatalf("stored flag changed after reconnect: %s", after)
	}
	check("DELETE", flag, "", 204)
	check("GET", flag, "", 404)
	check("DELETE", flag, "", 404)
}

// testAdmin подтверждает одну административную сессию без общего mutable state.
type testAdmin struct{}

// Authorize возвращает ID тестового администратора.
func (testAdmin) Authorize(context.Context, *identityv1.ValidateSessionRequest, ...grpc.CallOption) (*identityv1.OnlineConfAuthorization, error) {
	return &identityv1.OnlineConfAuthorization{UserId: 42}, nil
}
