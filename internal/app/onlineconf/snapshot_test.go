package onlineconf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProductView(t *testing.T) {
	value := snapshot{CheckedAtMS: 1000, Entries: map[string]SnapshotEntry{
		"bff.null":  {Scope: "bff", Type: "config", Data: map[string]json.RawMessage{"value": json.RawMessage("null")}},
		"bff.false": {Scope: "bff", Type: "config", Data: map[string]json.RawMessage{"value": json.RawMessage("false")}},
		"bff.zero":  {Scope: "bff", Type: "config", Data: map[string]json.RawMessage{"value": json.RawMessage("0")}},
		"workspace.off": {Scope: "workspace", Type: "flag", Data: map[string]json.RawMessage{
			"enabled": json.RawMessage("false"), "rollout_percent": json.RawMessage("100"),
			"include_users": json.RawMessage(`[{"id":42}]`), "exclude_users": json.RawMessage("[]"),
		}},
	}}

	view := newProductView(value, 61000)
	for _, name := range []string{"null", "false", "zero"} {
		entry, state := view.Lookup("bff." + name)
		expected := map[string]string{"null": "null", "false": "false", "zero": "0"}[name]
		if state != Found || string(entry.Data["value"]) != expected {
			t.Errorf("%s = %s, %v, want %s, found", name, entry.Data["value"], state, expected)
		}
	}
	if enabled, state := view.Flag("workspace.off", 42); enabled || state != Found {
		t.Errorf("disabled flag = %v, %v", enabled, state)
	}
	if _, state := view.Lookup("bff.absent"); state != Missing {
		t.Errorf("fresh absent = %v", state)
	}

	for _, expired := range []ProductView{newProductView(value, 61001), newProductView(value, 999), {}} {
		for _, key := range []string{"bff.null", "bff.absent"} {
			if _, state := expired.Lookup(key); state != Unavailable {
				t.Errorf("unavailable view for %s = %v", key, state)
			}
		}
	}
}

func TestKeysRefreshAfterCommit(t *testing.T) {
	first := testRedisSnapshots(t)
	second := testRedisSnapshots(t)
	second.prefix = first.prefix
	pool, openErr := pgxpool.New(t.Context(), onlineConfTestDatabase(t))
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(pool.Close)
	if err := migration.Up(t.Context(), pool, migration.OnlineConf); err != nil {
		t.Fatal(err)
	}
	store := keyStore{pool: pool}
	refresher := newSnapshotRefresher(store, first, testSnapshotLogger())
	refresher.period = time.Hour
	start := func(worker *snapshotRefresher) func() {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- worker.Run(ctx)
			close(done)
		}()
		stop := func() {
			cancel()
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
		t.Cleanup(stop)
		return stop
	}
	stop := start(refresher)
	waitForView(t, second, func(v ProductView) bool {
		_, state := v.Lookup("bff.limit")
		return state == Missing
	})

	notifications := 0
	var skipNotification bool
	request := func(repo keysRepository, method, name, body string, want int) {
		t.Helper()
		admin := newKeysHandler(repo, &testDirectory{}, func() {
			notifications++
			if !skipNotification {
				refresher.RequestRefresh()
			}
		}, nil)
		handler := newRouter(testAdmin{}, admin, "test")
		r := httptest.NewRequest(method, "/admin/keys"+name, strings.NewReader(body))
		r.Header.Set("X-BFF-Key", "test")
		r.Header.Set("Authorization", "Bearer test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, body %s, want %d", method, name, w.Code, w.Body, want)
		}
	}

	for _, tc := range []struct{ scope, name, create, update, config string }{
		{"bff", "limit", `{"name":"bff.limit","scope":"bff","type":"config","format":"json"}`, `{"description":"","value":[0,false,null,1.25]}`, `[0,false,null,1.25]`},
		{"workspace", "run", `{"name":"workspace.run","scope":"workspace","type":"flag"}`, `{"description":"","enabled":true,"rollout_percent":0,"include_user_ids":[42],"exclude_user_ids":[17]}`, ""},
	} {
		key := tc.scope + "." + tc.name
		request(store, http.MethodPost, "", tc.create, 201)
		waitForView(t, second, func(v ProductView) bool {
			if tc.config == "" {
				enabled, state := v.Flag(key, 42)
				return state == Found && !enabled
			}
			entry, state := v.Lookup(key)
			return state == Found && string(entry.Data["value"]) == "null"
		})
		request(store, http.MethodPut, "/"+key, tc.update, 200)
		waitForView(t, second, func(v ProductView) bool {
			if tc.config == "" {
				enabled, state := v.Flag(key, 42)
				entry, _ := v.Lookup(key)
				return state == Found && enabled && !strings.Contains(string(entry.Data["include_users"]), "login")
			}
			entry, state := v.Lookup(key)
			return state == Found && string(entry.Data["value"]) == tc.config
		})
		request(store, http.MethodDelete, "/"+key, "", 204)
		waitForView(t, second, func(v ProductView) bool {
			_, state := v.Lookup(key)
			return state == Missing
		})
	}

	request(store, http.MethodPost, "", `{"name":"bff.limit","scope":"bff","type":"config","format":"json"}`, 201)
	waitForView(t, second, func(v ProductView) bool {
		_, state := v.Lookup("bff.limit")
		return state == Found
	})
	before, _, err := second.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	count := notifications
	request(rolledBackDelete{store}, http.MethodDelete, "/bff.limit", "", 500)
	request(store, http.MethodDelete, "/bff.nonexistent", "", 404)
	if notifications != count {
		t.Fatal("failed writes triggered a refresh")
	}
	after, _, err := second.load(t.Context())
	if err != nil || after.CheckedAtMS != before.CheckedAtMS {
		t.Fatalf("failed transactions changed the snapshot: %v", err)
	}

	skipNotification = true
	request(store, http.MethodPut, "/bff.limit", `{"description":"","value":73}`, 200)
	if entry, state := readView(t, second).Lookup("bff.limit"); state != Found || string(entry.Data["value"]) != "null" {
		t.Fatalf("snapshot without publish = %+v, %v", entry, state)
	}
	stop()
	restarted := newSnapshotRefresher(store, first, testSnapshotLogger())
	start(restarted)
	waitForView(t, second, func(v ProductView) bool {
		entry, state := v.Lookup("bff.limit")
		return state == Found && string(entry.Data["value"]) == "73"
	})
}

func TestSnapshotCommitDuringRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &testSnapshotSource{started: make(chan struct{}), unblock: make(chan struct{}), value: "3"}
		cache := &testSnapshotBackend{}
		refresher := newSnapshotRefresher(source, cache, testSnapshotLogger())
		refresher.period = time.Hour
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- refresher.Run(ctx) }()
		<-source.started
		source.mu.Lock()
		source.value = "4"
		source.mu.Unlock()
		refresher.RequestRefresh()
		refresher.RequestRefresh()
		close(source.unblock)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if source.calls != 2 || len(cache.published) != 2 ||
			string(cache.published[1].Entries["bff.limit"].Data["value"]) != "4" {
			t.Fatalf("calls = %d, snapshots = %v", source.calls, cache.published)
		}
	})
}

// rolledBackDelete подтверждает удаление только внутри транзакции и откатывает его до возврата.
type rolledBackDelete struct{ keyStore }

func (s rolledBackDelete) delete(ctx context.Context, name string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "DELETE FROM onlineconf.keys WHERE name = $1", name); err != nil {
		return err
	}
	if err := tx.Rollback(ctx); err != nil {
		return err
	}
	return errors.New("test rollback")
}
