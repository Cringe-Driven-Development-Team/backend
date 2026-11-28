package onlineconf

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const (
	refreshPeriod  = 15 * time.Second
	refreshTimeout = 3 * time.Second
	busyRetry      = 200 * time.Millisecond
)

var errRefreshBusy = errors.New("OnlineConf snapshot lease is busy")

type snapshotSource interface {
	readSnapshot(context.Context) (map[string]SnapshotEntry, error)
}

type snapshotPublisher interface {
	acquire(context.Context, string) (int64, bool, error)
	publish(context.Context, string, snapshot) (bool, error)
	release(context.Context, string) error
}

type snapshotRefresher struct {
	source    snapshotSource
	cache     snapshotPublisher
	logger    *slog.Logger
	requested chan struct{}
	period    time.Duration
}

func newSnapshotRefresher(source snapshotSource, cache snapshotPublisher, logger *slog.Logger) *snapshotRefresher {
	return &snapshotRefresher{source: source, cache: cache, logger: logger, requested: make(chan struct{}, 1), period: refreshPeriod}
}

// RequestRefresh будит единственный worker после подтверждённой записи в PostgreSQL.
func (r *snapshotRefresher) RequestRefresh() {
	select {
	case r.requested <- struct{}{}:
	default:
	}
}

// refreshOnce проверяет источник и публикует снимок только при действующем токене.
func (r *snapshotRefresher) refreshOnce(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, refreshTimeout)
	defer cancel()
	token := uuid.NewString()
	redisMS, acquired, err := r.cache.acquire(ctx, token)
	if err != nil {
		return err
	}
	if !acquired {
		return errRefreshBusy
	}
	published := false
	defer func() {
		if !published && ctx.Err() == nil {
			_ = r.cache.release(ctx, token)
		}
	}()

	started := time.Now()
	entries, err := r.source.readSnapshot(ctx)
	if err != nil {
		return err
	}
	checked := time.Now()
	checkedAt := redisMS + checked.Sub(started).Milliseconds()
	published, err = r.cache.publish(ctx, token, snapshot{CheckedAtMS: checkedAt, Entries: entries})
	if err == nil && !published {
		return errRefreshBusy
	}
	if err == nil {
		r.logger.DebugContext(ctx, "onlineconf.snapshot.refresh", slog.String("result", "published"),
			slog.Duration("duration", time.Since(started)), slog.Duration("snapshot_age", time.Since(checked)))
	}
	return err
}

// Run сериализует стартовую, периодическую и запрошенную после записи сверки.
func (r *snapshotRefresher) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	requestPending := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		case <-r.requested:
			requestPending = true
		}
		timer.Stop()
		// Сигнал до чтения покрывает текущая попытка. Сигнал во время чтения
		// остаётся в канале и будит worker после окончания публикации.
		select {
		case <-r.requested:
			requestPending = true
		default:
		}
		if ctx.Err() != nil {
			return nil
		}

		started := time.Now()
		err := r.refreshOnce(ctx)
		delay := r.period
		switch {
		case errors.Is(err, errRefreshBusy) && requestPending:
			delay = busyRetry
		case errors.Is(err, errRefreshBusy):
			// Другой экземпляр выполняет периодическую сверку.
		case err != nil:
			if ctx.Err() == nil {
				r.logger.WarnContext(ctx, "onlineconf.snapshot.refresh", slog.String("result", "failed"), slog.Duration("duration", time.Since(started)))
			}
		}
		requestPending = errors.Is(err, errRefreshBusy) && requestPending
		timer.Reset(delay)
	}
}
