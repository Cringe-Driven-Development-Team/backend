package onlineconf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/productconfig"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const evaluatePath = "/admin/keys/workspace.notebook_run_all/evaluate"

type evaluationDirectory struct {
	identityv1.UsersClient
	ids     []int64
	missing bool
	err     error
}

func (d *evaluationDirectory) BatchGetUsers(_ context.Context, req *identityv1.BatchGetUsersRequest, _ ...grpc.CallOption) (*identityv1.UsersResponse, error) {
	d.ids = append(d.ids, req.GetUserIds()...)
	result := &identityv1.UsersResponse{}
	if !d.missing {
		for _, id := range req.GetUserIds() {
			result.Users = append(result.Users, &identityv1.User{Id: id})
		}
	}
	return result, d.err
}

func evaluateRequest(router http.Handler, path, key, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		r.Header.Set("X-BFF-Key", key)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func assertEvaluateError(t *testing.T, w *httptest.ResponseRecorder, code int, want string) {
	t.Helper()
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if w.Code != code || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != want || body.Message == "" {
		t.Fatalf("response = %d %s, want %d %s", w.Code, w.Body, code, want)
	}
}

func storedEvaluationFlag(excluded string) SnapshotEntry {
	return SnapshotEntry{Scope: "workspace", Type: "flag", Data: map[string]json.RawMessage{
		"enabled": json.RawMessage("true"), "rollout_percent": json.RawMessage("100"),
		"include_users": json.RawMessage("[]"), "exclude_users": json.RawMessage(excluded),
	}}
}

func TestAdminEvaluate(t *testing.T) {
	for _, tc := range []struct {
		name, path, key, token string
		auth                   codes.Code
		missing                bool
		directoryErr           error
		id                     int64
		want                   int
		code                   string
	}{
		{"excluded", evaluatePath + "?user_id=42", "bff", "admin", codes.OK, false, nil, 42, 200, ""},
		{"max int64", evaluatePath + "?user_id=9223372036854775807", "bff", "admin", codes.OK, false, nil, 9223372036854775807, 200, ""},
		{"missing key", evaluatePath + "?user_id=42", "", "admin", codes.OK, false, nil, 0, 403, "s2s_forbidden"},
		{"wrong key", evaluatePath + "?user_id=42", "wrong", "admin", codes.OK, false, nil, 0, 403, "s2s_forbidden"},
		{"missing session", evaluatePath + "?user_id=42", "bff", "", codes.OK, false, nil, 0, 401, "unauthorized"},
		{"revoked", evaluatePath + "?user_id=42", "bff", "revoked", codes.Unauthenticated, false, nil, 0, 401, "unauthorized"},
		{"missing role", evaluatePath + "?user_id=42", "bff", "regular", codes.PermissionDenied, false, nil, 0, 403, "forbidden"},
		{"role check unavailable", evaluatePath + "?user_id=42", "bff", "admin", codes.Unavailable, false, nil, 0, 503, "dependency_unavailable"},
		{"missing selected user", evaluatePath + "?user_id=42", "bff", "admin", codes.OK, true, nil, 42, 404, "not_found"},
		{"directory unavailable", evaluatePath + "?user_id=42", "bff", "admin", codes.OK, false, status.Error(codes.Unavailable, "test"), 42, 503, "dependency_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := &evaluationDirectory{missing: tc.missing, err: tc.directoryErr}
			reads := 0
			admin := newKeysHandler(nil, directory, nil, func(context.Context) (ProductView, error) {
				reads++
				return ProductView{entries: map[string]SnapshotEntry{"workspace.notebook_run_all": storedEvaluationFlag(`[{"id":42}]`)}}, nil
			})
			auth := &accessClientStub{userID: 17, err: status.Error(tc.auth, "test")}
			w := evaluateRequest(newRouter(auth, admin, "bff"), tc.path, tc.key, tc.token)
			if tc.want != 200 {
				assertEvaluateError(t, w, tc.want, tc.code)
				if reads != 0 {
					t.Fatal("snapshot read after failed authorization or directory lookup")
				}
			} else {
				want := `{"enabled":false,"reason":"excluded"}`
				if tc.id != 42 {
					want = `{"enabled":true,"reason":"rollout"}`
				}
				if w.Code != 200 || w.Body.String() != want+"\n" || reads != 1 ||
					w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("response = %d %s, reads = %d, headers = %v", w.Code, w.Body, reads, w.Header())
				}
			}
			if tc.id > 0 && (len(directory.ids) != 1 || directory.ids[0] != tc.id) || tc.id == 0 && len(directory.ids) != 0 {
				t.Fatalf("directory called with %v, expected selected user %d", directory.ids, tc.id)
			}
		})
	}
}

func TestAdminEvaluateInvalidParameters(t *testing.T) {
	directory := &evaluationDirectory{}
	admin := newKeysHandler(nil, directory, nil, func(context.Context) (ProductView, error) {
		t.Fatal("read snapshot for invalid parameters")
		return ProductView{}, nil
	})
	router := newRouter(&accessClientStub{userID: 17}, admin, "bff")
	for _, suffix := range []string{"", "?user_id=", "?user_id=0", "?user_id=-1", "?user_id=%2B1", "?user_id=1.5", "?user_id=9223372036854775808", "?user_id=1&user_id=2", "?user_id=1&extra=2", "?user_id=%FF"} {
		assertEvaluateError(t, evaluateRequest(router, evaluatePath+suffix, "bff", "admin"), 400, "validation_error")
	}
	for _, key := range []string{"invalid", "other.key", "workspace.bad-name"} {
		assertEvaluateError(t, evaluateRequest(router, "/admin/keys/"+key+"/evaluate?user_id=42", "bff", "admin"), 400, "validation_error")
	}
	if len(directory.ids) != 0 {
		t.Fatalf("catalog called for invalid parameters: %v", directory.ids)
	}
}

func TestAdminEvaluateSnapshot(t *testing.T) {
	snapshotValue := snapshot{CheckedAtMS: 1000, Entries: map[string]SnapshotEntry{
		"workspace.notebook_run_all": storedEvaluationFlag("[]"),
		"bff.notebook_max_cells":     {Scope: "bff", Type: "config", Data: map[string]json.RawMessage{"value": json.RawMessage("3")}},
		"workspace.bad_flag":         {Scope: "workspace", Type: "flag"},
	}}
	for _, tc := range []struct {
		name, path string
		view       ProductView
		err        error
		want       int
		code       string
	}{
		{"fresh at 60 seconds", evaluatePath, newProductView(snapshotValue, 61000), nil, 200, ""},
		{"config", "/admin/keys/bff.notebook_max_cells/evaluate", newProductView(snapshotValue, 61000), nil, 400, "validation_error"},
		{"missing flag", "/admin/keys/workspace.unknown/evaluate", newProductView(snapshotValue, 61000), nil, 404, "not_found"},
		{"stale flag", evaluatePath, newProductView(snapshotValue, 61001), nil, 503, "onlineconf_unavailable"},
		{"stale and missing", "/admin/keys/workspace.unknown/evaluate", newProductView(snapshotValue, 61001), nil, 503, "onlineconf_unavailable"},
		{"absent snapshot", evaluatePath, ProductView{}, nil, 503, "onlineconf_unavailable"},
		{"Redis error", evaluatePath, ProductView{}, errors.New("test cache error"), 503, "onlineconf_unavailable"},
		{"invalid rules", "/admin/keys/workspace.bad_flag/evaluate", newProductView(snapshotValue, 61000), nil, 503, "onlineconf_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admin := newKeysHandler(nil, &evaluationDirectory{}, nil, func(context.Context) (ProductView, error) {
				return tc.view, tc.err
			})
			w := evaluateRequest(newRouter(&accessClientStub{userID: 17}, admin, "bff"), tc.path+"?user_id=42", "bff", "admin")
			if tc.want != 200 {
				assertEvaluateError(t, w, tc.want, tc.code)
			} else if w.Code != 200 || w.Body.String() != "{\"enabled\":true,\"reason\":\"rollout\"}\n" {
				t.Fatalf("fresh snapshot = %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestAdminEvaluateProductParity(t *testing.T) {
	cache := testRedisSnapshots(t)
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, onlineConfTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if migrateErr := migration.Up(ctx, pool, migration.OnlineConf); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	store := keyStore{pool: pool}
	entry := storedEvaluationFlag(`[{"id":2}]`)
	ref := userRef{ID: 17, Login: "admin"}
	flag := keyRecord{Name: "workspace.notebook_run_all", Scope: entry.Scope, Type: entry.Type, Data: entry.Data, CreatedBy: ref, UpdatedBy: ref}
	if _, createErr := store.create(ctx, flag); createErr != nil {
		t.Fatal(createErr)
	}

	refresher := newSnapshotRefresher(store, cache, testSnapshotLogger())
	if refreshErr := refresher.refreshOnce(ctx); refreshErr != nil {
		t.Fatal(refreshErr)
	}
	admin := newKeysHandler(nil, &evaluationDirectory{}, func() {
		t.Fatal("evaluation requested a refresh")
	}, cache.ProductView)
	router := newRouter(&accessClientStub{userID: 17}, admin, "bff", productHandler(productSessions{}, cache.ProductView, "bff", "workspace"))

	compare := func(query, token string, enabled bool, reason string) {
		t.Helper()
		a := evaluateRequest(router, evaluatePath+"?user_id="+query, "bff", "admin")
		p := evaluateRequest(router, productconfig.Path, "bff", token)
		var actual struct {
			Enabled *bool  `json:"enabled"`
			Reason  string `json:"reason"`
		}
		var product struct {
			RunAll *bool `json:"notebook_run_all"`
		}
		if a.Code != 200 || json.Unmarshal(a.Body.Bytes(), &actual) != nil || actual.Enabled == nil ||
			*actual.Enabled != enabled || actual.Reason != reason || p.Code != 200 ||
			json.Unmarshal(p.Body.Bytes(), &product) != nil || product.RunAll == nil || *product.RunAll != *actual.Enabled {
			t.Fatalf("admin = %d %s, product = %d %s, want %t %s", a.Code, a.Body, p.Code, p.Body, enabled, reason)
		}
	}
	before, _, err := cache.load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	compare("1", "a", true, "rollout")
	compare("2", "b", false, "excluded")

	flag.Data["rollout_percent"] = json.RawMessage("25")
	flag.Data["include_users"] = json.RawMessage(`[{"id":2}]`)
	flag.Data["exclude_users"] = json.RawMessage("[]")
	if _, updateErr := store.update(ctx, flag); updateErr != nil {
		t.Fatal(updateErr)
	}
	compare("1", "a", true, "rollout")
	compare("2", "b", false, "excluded")
	unchanged, _, err := cache.load(ctx)
	if err != nil || unchanged.CheckedAtMS != before.CheckedAtMS {
		t.Fatalf("reads changed snapshot freshness: %d to %d, %v", before.CheckedAtMS, unchanged.CheckedAtMS, err)
	}
	if refreshErr := refresher.refreshOnce(ctx); refreshErr != nil {
		t.Fatal(refreshErr)
	}
	// For workspace.notebook_run_all and user 1, the stable bucket is 27.
	compare("1", "a", false, "rollout")
	compare("2", "b", true, "included")

	if deleteErr := cache.client.Del(ctx, cache.prefix+":snapshot").Err(); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	assertEvaluateError(t, evaluateRequest(router, evaluatePath+"?user_id=1", "bff", "admin"), 503, "onlineconf_unavailable")
}
