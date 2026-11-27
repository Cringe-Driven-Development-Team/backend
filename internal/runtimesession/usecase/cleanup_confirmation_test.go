package usecase

import (
	"context"
	"testing"
	"time"

	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/google/uuid"
)

// cleanupStore фиксирует итоговые состояния без зависимости теста от числа checkpoint шагов.
type cleanupStore struct {
	Store
	saved []D.Session
}

func (*cleanupStore) CheckLease(context.Context, D.Lease) error  { return nil }
func (*cleanupStore) MarkUnknown(context.Context, D.Lease) error { return nil }
func (s *cleanupStore) Checkpoint(_ context.Context, _ D.Lease, v D.Session) error {
	s.saved = append(s.saved, v)
	return nil
}
func (s *cleanupStore) Save(_ context.Context, _ D.Lease, v D.Session) error {
	s.saved = append(s.saved, v)
	return nil
}

// cleanupEnvironments отличает отсутствие записи, terminal state и фактическое подтверждение очистки.
type cleanupEnvironments struct {
	state   D.Environment
	err     error
	deletes int
}

func (e *cleanupEnvironments) Get(context.Context, uuid.UUID) (D.Environment, error) {
	return e.state, e.err
}
func (e *cleanupEnvironments) RequestCreate(context.Context, D.EnvironmentRequest) (D.Environment, error) {
	panic("cleanup must not re-create environment")
}
func (e *cleanupEnvironments) RequestDelete(context.Context, uuid.UUID, string) error {
	e.deletes++
	return nil
}

// TestCleanupRequiresConfirmation запрещает освобождать слот по NotFound/FAILED/DELETED без marker.
func TestCleanupRequiresConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, state        string
		missing, confirmed bool
	}{{name: "missing", missing: true}, {name: "failed", state: "FAILED"}, {name: "deleted without confirmation", state: "DELETED"}, {name: "confirmed deleted", state: "DELETED", confirmed: true}, {name: "confirmed failed cleanup", state: "FAILED", confirmed: true}} {
		t.Run(tc.name, func(t *testing.T) {
			store := &cleanupStore{}
			environments := &cleanupEnvironments{state: D.Environment{State: tc.state, CleanupConfirmed: tc.confirmed}}
			if tc.missing {
				environments.err = D.ErrNotFound
			}
			worker := NewWorker(store, environments, nil, time.Now)
			v := D.Session{ID: uuid.New(), OwnerID: 42, State: D.Stopping, CreateSent: true}
			if err := worker.Step(t.Context(), D.Lease{Session: v, Version: 1}); err != nil {
				t.Fatal(err)
			}
			if len(store.saved) == 0 {
				t.Fatal("cleanup state not persisted")
			}
			saved := store.saved[len(store.saved)-1]
			if tc.confirmed {
				if saved.CleanupError || saved.State.Active() {
					t.Fatal("confirmed cleanup did not release")
				}
			} else {
				if !saved.CleanupError {
					t.Fatal("unconfirmed cleanup released user slot")
				}
				if environments.deletes != 1 {
					t.Fatal("delete/tombstone intent not sent")
				}
			}
		})
	}
}
