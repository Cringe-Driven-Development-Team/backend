package postgres

import (
	"errors"
	"testing"

	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
)

// TestTerminalPendingStillOwnsSlot сохраняет слот после STOPPED до подтверждённой очистки.
func TestTerminalPendingStillOwnsSlot(t *testing.T) {
	store, pool, owner, notebook := integrationStore(t)
	ctx := t.Context()
	first, err := store.Start(ctx, newSession(owner, notebook))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Mutate(ctx, owner, first.ID, func(v *D.Session) error { v.State = D.Stopped; v.CleanupError = true; return nil }); err != nil {
		t.Fatal(err)
	}
	active, err := store.Active(ctx, owner)
	if err != nil || active == nil || active.ID != first.ID || !active.CleanupError {
		t.Fatalf("cleanup slot lost: %+v %v", active, err)
	}
	_, err = store.Start(ctx, newSession(owner, notebook))
	if !errors.Is(err, D.ErrActiveSessionExists) {
		t.Fatalf("second environment allowed: %v", err)
	}
	lease, err := store.Claim(ctx, "cleanup-test")
	if err != nil {
		t.Fatal(err)
	}
	closed := lease.Session
	closed.CleanupError = false
	if err = store.Save(ctx, lease, closed); err != nil {
		t.Fatal(err)
	}
	if active, err = store.Active(ctx, owner); err != nil || active != nil {
		t.Fatalf("confirmed cleanup did not release: %+v %v", active, err)
	}
	next, err := store.Start(ctx, newSession(owner, notebook))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(ctx, lease, closed); !errors.Is(err, D.ErrLeaseLost) {
		t.Fatalf("stale worker accepted: %v", err)
	}
	var held string
	if err = pool.QueryRow(ctx, "SELECT active_session_id::text FROM workspace.user_guards WHERE user_id=$1", owner).Scan(&held); err != nil || held != next.ID.String() {
		t.Fatalf("stale cleanup released a newer slot: %s %v", held, err)
	}
}
