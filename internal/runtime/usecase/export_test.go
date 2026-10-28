package usecase

import (
	"context"
	"time"
)

// SetVerifyInterval задаёт период опроса /health в тестах (по умолчанию 5 с).
func SetVerifyInterval(w *Worker, d time.Duration) { w.verifyInterval = d }

// Reconcile выполняет один проход поиска сирот.
func Reconcile(ctx context.Context, w *Worker) error { return w.reconcile(ctx) }
