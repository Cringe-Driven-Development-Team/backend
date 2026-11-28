package onlineconf

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func testRedisSnapshots(t *testing.T) *redisSnapshots {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR to run Redis tests")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1})
	prefix := "{onlineconf-test:" + uuid.NewString() + "}"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Del(ctx, prefix+":snapshot", prefix+":refresh").Err()
		_ = client.Close()
	})
	return &redisSnapshots{client: client, prefix: prefix}
}

func TestRedisSnapshotAtomicPublish(t *testing.T) {
	cache := testRedisSnapshots(t)
	ctx := t.Context()
	if _, state := readView(t, cache).Lookup("bff.limit"); state != Unavailable {
		t.Fatalf("missing snapshot = %v", state)
	}
	if _, acquired, err := cache.acquire(ctx, "old"); err != nil || !acquired {
		t.Fatalf("acquire = %v, %v", acquired, err)
	}
	if err := cache.client.Del(ctx, cache.prefix+":refresh").Err(); err != nil {
		t.Fatal(err)
	}
	checkTime, acquired, acquireErr := cache.acquire(ctx, "new")
	if acquireErr != nil || !acquired {
		t.Fatalf("new acquire = %v, %v", acquired, acquireErr)
	}
	if err := cache.release(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	if ok, err := cache.publish(ctx, "old", snapshot{CheckedAtMS: checkTime, Entries: testSnapshotEntries("1")}); err != nil || ok {
		t.Fatalf("old publish = %v, %v", ok, err)
	}
	if ok, err := cache.publish(ctx, "new", snapshot{CheckedAtMS: checkTime, Entries: testSnapshotEntries("2")}); err != nil || !ok {
		t.Fatalf("new publish = %v, %v", ok, err)
	}
	if entry, state := readView(t, cache).Lookup("bff.limit"); state != Found || string(entry.Data["value"]) != "2" {
		t.Fatalf("published entry = %+v, %v", entry, state)
	}
	if err := cache.client.PExpire(ctx, cache.prefix+":snapshot", 500*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	_ = readView(t, cache)
	if ttl := cache.client.PTTL(ctx, cache.prefix+":snapshot").Val(); ttl > 500*time.Millisecond {
		t.Fatalf("read renewed TTL: %v", ttl)
	}
	if err := cache.client.PExpireAt(ctx, cache.prefix+":snapshot", time.UnixMilli(1)).Err(); err != nil {
		t.Fatal(err)
	}
	if _, state := readView(t, cache).Lookup("bff.limit"); state != Unavailable {
		t.Fatalf("expired snapshot = %v", state)
	}
	refresher := newSnapshotRefresher(&testSnapshotSource{value: "4"}, cache, testSnapshotLogger())
	if err := refresher.refreshOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if entry, state := readView(t, cache).Lookup("bff.limit"); state != Found || string(entry.Data["value"]) != "4" {
		t.Fatalf("recovered snapshot = %+v, %v", entry, state)
	}
	if _, state := readView(t, cache).Lookup("bff.other"); state != Missing {
		t.Fatalf("missing key in valid snapshot = %v", state)
	}

	if err := cache.client.Close(); err != nil {
		t.Fatal(err)
	}
	if view, err := cache.ProductView(ctx); err == nil {
		t.Fatal("unavailable Redis returned success")
	} else if _, state := view.Lookup("bff.limit"); state != Unavailable {
		t.Fatalf("unavailable Redis = %v", state)
	}
	reconnected := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_TEST_ADDR"), MaxRetries: -1})
	t.Cleanup(func() {
		_ = reconnected.Del(context.Background(), cache.prefix+":snapshot", cache.prefix+":refresh").Err()
		_ = reconnected.Close()
	})
	cache.client = reconnected
	if err := refresher.refreshOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, state := readView(t, cache).Lookup("bff.limit"); state != Found {
		t.Fatalf("Redis recovery = %v", state)
	}
}

func readView(t *testing.T, cache *redisSnapshots) ProductView {
	t.Helper()
	view, err := cache.ProductView(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func waitForView(t *testing.T, cache *redisSnapshots, condition func(ProductView) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if view := readView(t, cache); condition(view) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("snapshot did not reach expected state")
}

func testSnapshotLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testSnapshotEntries(value string) map[string]SnapshotEntry {
	return map[string]SnapshotEntry{"bff.limit": {Scope: "bff", Type: "config", Data: map[string]json.RawMessage{"value": json.RawMessage(value)}}}
}

type testSnapshotSource struct {
	mu               sync.Mutex
	value            string
	calls            int
	started, unblock chan struct{}
	err              error
}

func (s *testSnapshotSource) readSnapshot(ctx context.Context) (map[string]SnapshotEntry, error) {
	s.mu.Lock()
	s.calls++
	value := s.value
	first := s.calls == 1
	readErr := s.err
	s.mu.Unlock()
	if first && s.started != nil {
		close(s.started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.unblock:
		}
	}
	if readErr != nil {
		return nil, readErr
	}
	return testSnapshotEntries(value), nil
}

type testSnapshotBackend struct {
	mu        sync.Mutex
	busy      bool
	fail      bool
	published []snapshot
}

func (c *testSnapshotBackend) acquire(context.Context, string) (int64, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().UnixMilli(), !c.busy, nil
}
func (c *testSnapshotBackend) publish(_ context.Context, _ string, value snapshot) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return false, errors.New("test Redis failure")
	}
	c.published = append(c.published, value)
	return true, nil
}
func (*testSnapshotBackend) release(context.Context, string) error { return nil }

func TestSnapshotRefreshRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &testSnapshotSource{value: "8"}
		cache := &testSnapshotBackend{fail: true}
		refresher := newSnapshotRefresher(source, cache, testSnapshotLogger())
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- refresher.Run(ctx) }()
		synctest.Wait()
		source.mu.Lock()
		calls := source.calls
		source.mu.Unlock()
		cache.mu.Lock()
		startupPublished := len(cache.published)
		cache.fail = false
		cache.mu.Unlock()
		if calls != 1 || startupPublished != 0 {
			t.Fatalf("failed startup publication: calls=%d, snapshots=%d", calls, startupPublished)
		}
		time.Sleep(15 * time.Second)
		synctest.Wait()
		cache.mu.Lock()
		periodic := append([]snapshot(nil), cache.published...)
		cache.busy = true
		cache.mu.Unlock()
		if len(periodic) != 1 || string(periodic[0].Entries["bff.limit"].Data["value"]) != "8" {
			t.Fatalf("periodic recovery = %+v", periodic)
		}
		refresher.RequestRefresh()
		synctest.Wait()
		cache.mu.Lock()
		cache.busy = false
		cache.mu.Unlock()
		source.mu.Lock()
		source.value = "9"
		source.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if len(cache.published) != 2 || string(cache.published[1].Entries["bff.limit"].Data["value"]) != "9" {
			t.Fatalf("busy retry = %+v", cache.published)
		}
	})
}

func TestSnapshotFreshnessAfterSourceFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &testSnapshotSource{value: "8"}
		cache := &testSnapshotBackend{}
		refresher := newSnapshotRefresher(source, cache, testSnapshotLogger())
		if err := refresher.refreshOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		source.err = errors.New("test PostgreSQL failure")
		for range 4 {
			time.Sleep(15 * time.Second)
			if err := refresher.refreshOnce(t.Context()); err == nil {
				t.Fatal("refresh with unavailable source succeeded")
			}
		}
		if len(cache.published) != 1 {
			t.Fatalf("failed refresh changed snapshots: %d", len(cache.published))
		}
		prior := cache.published[0]
		if _, state := newProductView(prior, time.Now().UnixMilli()).Lookup("bff.limit"); state != Found {
			t.Fatalf("snapshot at exactly 60 seconds = %v", state)
		}
		time.Sleep(time.Millisecond)
		if _, state := newProductView(prior, time.Now().UnixMilli()).Lookup("bff.unknown"); state != Unavailable {
			t.Fatalf("missing key in stale snapshot = %v", state)
		}

		source.err = nil
		if err := refresher.refreshOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(cache.published) != 2 {
			t.Fatalf("recovery did not publish unchanged data: %d", len(cache.published))
		}
		latest := cache.published[1]
		entry, state := newProductView(latest, time.Now().UnixMilli()).Lookup("bff.limit")
		if state != Found || string(entry.Data["value"]) != "8" {
			t.Fatalf("after recovery = %+v, %v", entry, state)
		}
	})
}

func TestSnapshotRedisConfig(t *testing.T) {
	config := map[string]string{"REDIS_HOST": "::1", "REDIS_PORT": "6380", "REDIS_DB": "2"}
	lookup := func(key string) (string, bool) {
		value, ok := config[key]
		return value, ok
	}
	opts, err := snapshotRedisOptions(lookup)
	if err != nil || opts.Addr != "[::1]:6380" || opts.DB != 2 {
		t.Fatalf("Redis address not parsed: %v", err)
	}
	config["REDIS_DB"] = "-1"
	if _, err := snapshotRedisOptions(lookup); err == nil {
		t.Fatal("accepted negative Redis DB")
	}
}
