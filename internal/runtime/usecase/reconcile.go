package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// reconcileLoop ищет сирот при старте и каждые ReconcileInterval до отмены ctx.
// Просроченные lease возвращает в работу сам Claim, незавершённые окружения продолжают их задания.
func (w *Worker) reconcileLoop(ctx context.Context) {
	t := time.NewTicker(w.s.ReconcileInterval)
	defer t.Stop()
	for {
		if err := w.reconcile(ctx); err != nil && ctx.Err() == nil {
			w.log.Error("reconcile", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// reconcile удаляет ресурсы рантайма, чья сессия неизвестна БД или уже DELETED/FAILED.
// ListOwned возвращает только ресурсы этого окружения (cellestial_env); ресурсы без session_id
// не трогаются.
func (w *Worker) reconcile(ctx context.Context) error {
	owned, err := w.cloud.ListOwned(ctx)
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}
	var ids []uuid.UUID
	for _, r := range owned {
		if r.SessionID != uuid.Nil && !slices.Contains(ids, r.SessionID) {
			ids = append(ids, r.SessionID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	known, err := w.repo.KnownSessions(ctx, ids)
	if err != nil {
		return fmt.Errorf("known sessions: %w", err)
	}
	var errs []error
	for _, kind := range deleteOrder() {
		for _, r := range owned {
			if r.Kind != kind || r.SessionID == uuid.Nil {
				continue
			}
			if st, ok := known[r.SessionID]; ok && !st.Finished() {
				continue
			}
			if err := w.deleteResource(ctx, r.Kind, r.ID); err != nil {
				errs = append(errs, fmt.Errorf("delete orphan %s %s: %w", r.Kind, r.ID, err))
				continue
			}
			w.log.Info("orphan deleted", "kind", r.Kind, "id", r.ID, "session_id", r.SessionID)
		}
	}
	return errors.Join(errs...)
}
