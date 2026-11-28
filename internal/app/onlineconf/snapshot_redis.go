package onlineconf

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	leaseTTL    = 5 * time.Second
	snapshotTTL = 120 * time.Second
)

const acquireSnapshotLease = `
if not redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2]) then
  return 0
end
local now = redis.call("TIME")
return tonumber(now[1])*1000 + math.floor(tonumber(now[2])/1000)
`

const publishSnapshot = `
if redis.call("GET", KEYS[1]) ~= ARGV[1] then
  return 0
end
redis.call("SET", KEYS[2], ARGV[2], "PX", ARGV[3])
redis.call("DEL", KEYS[1])
return 1
`

const releaseSnapshotLease = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`

const readSnapshotAndTime = `
local value = redis.call("GET", KEYS[1])
if not value then
  return {}
end
local now = redis.call("TIME")
return {value, tonumber(now[1])*1000 + math.floor(tonumber(now[2])/1000)}
`

// redisSnapshots хранит общий снимок и сериализует публикации между экземплярами.
type redisSnapshots struct {
	client *redis.Client
	prefix string
}

func (r *redisSnapshots) acquire(ctx context.Context, token string) (int64, bool, error) {
	ms, err := r.client.Eval(ctx, acquireSnapshotLease, []string{r.prefix + ":refresh"}, token, leaseTTL.Milliseconds()).Int64()
	return ms, ms > 0, err
}

func (r *redisSnapshots) publish(ctx context.Context, token string, value snapshot) (bool, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	result, err := r.client.Eval(ctx, publishSnapshot, []string{r.prefix + ":refresh", r.prefix + ":snapshot"},
		token, payload, snapshotTTL.Milliseconds()).Int64()
	return result == 1, err
}

func (r *redisSnapshots) release(ctx context.Context, token string) error {
	return r.client.Eval(ctx, releaseSnapshotLease, []string{r.prefix + ":refresh"}, token).Err()
}

func (r *redisSnapshots) load(ctx context.Context) (snapshot, int64, error) {
	result, err := r.client.Eval(ctx, readSnapshotAndTime, []string{r.prefix + ":snapshot"}).Slice()
	if err != nil || len(result) == 0 {
		return snapshot{}, 0, err
	}
	if len(result) != 2 {
		return snapshot{}, 0, errInvalidSnapshot
	}
	payload, payloadOK := result[0].(string)
	nowMS, timeOK := result[1].(int64)
	if !payloadOK || !timeOK {
		return snapshot{}, 0, errInvalidSnapshot
	}
	var value snapshot
	if err := json.Unmarshal([]byte(payload), &value); err != nil || value.Entries == nil || value.CheckedAtMS <= 0 {
		return snapshot{}, 0, errInvalidSnapshot
	}
	return value, nowMS, nil
}

// ProductView читает один снимок из Redis. Ошибка чтения даёт недоступный view.
func (r *redisSnapshots) ProductView(ctx context.Context) (ProductView, error) {
	value, nowMS, err := r.load(ctx)
	if err != nil {
		return ProductView{}, err
	}
	view := newProductView(value, nowMS)
	for name, entry := range view.entries {
		if !entry.valid(name) {
			return ProductView{}, errInvalidSnapshot
		}
	}
	return view, nil
}
