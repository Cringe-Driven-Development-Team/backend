package onlineconf

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func snapshotRedisOptions(lookup func(string) (string, bool)) (*redis.Options, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	host := get("REDIS_HOST", "redis")
	port, err := strconv.Atoi(get("REDIS_PORT", "6379"))
	if err != nil || port < 1 || port > 65535 || host == "" || strings.ContainsAny(host, " /\t\r\n") {
		return nil, errors.New("invalid Redis address")
	}
	db, err := strconv.Atoi(get("REDIS_DB", "0"))
	if err != nil || db < 0 {
		return nil, errors.New("invalid REDIS_DB")
	}
	return &redis.Options{
		Addr: net.JoinHostPort(host, strconv.Itoa(port)), Password: get("REDIS_PASSWORD", ""), DB: db,
		MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		PoolTimeout: time.Second, ContextTimeoutEnabled: true,
	}, nil
}
