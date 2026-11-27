package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	U "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/google/uuid"
)

// cleanupEnvironment имитирует временный отказ облака и подтверждение удаления после восстановления.
type cleanupEnvironment struct {
	deleted  bool
	requests int
}

// Get возвращает наблюдаемое состояние облачного окружения.
func (e *cleanupEnvironment) Get(_ context.Context, id uuid.UUID) (D.Environment, error) {
	state := "READY"
	if e.deleted {
		state = "DELETED"
	}
	return D.Environment{SessionID: id, State: state, CleanupConfirmed: e.deleted}, nil
}

// RequestCreate не используется при завершении существующей сессии.
func (*cleanupEnvironment) RequestCreate(context.Context, D.EnvironmentRequest) (D.Environment, error) {
	return D.Environment{}, errors.New("unexpected create")
}

// RequestDelete сохраняет попытку удаления и воспроизводит временную недоступность облака.
func (e *cleanupEnvironment) RequestDelete(context.Context, uuid.UUID, string) error {
	e.requests++
	return errors.New("cloud unavailable")
}

// cleanupAgent подтверждает остановку и учитывает повторные обращения после завершения пользовательской сессии.
type cleanupAgent struct {
	U.SessionAgent
	stops int
}

// Drain запрещает приём новых команд в тестовом агенте.
func (*cleanupAgent) Drain(context.Context, uuid.UUID) error { return nil }

// Stop подтверждает остановку kernel.
func (a *cleanupAgent) Stop(context.Context, uuid.UUID) error { a.stops++; return nil }

// Events подтверждает, что все доступные события уже сохранены.
func (*cleanupAgent) Events(context.Context, uuid.UUID, int64, D.EventLimits) ([]D.AgentEvent, error) {
	return []D.AgentEvent{}, nil
}

// TestCleanupFailureRetainsSlotAndSurvivesRestart проверяет удержание слота при сбое облака и восстановление очистки.
func TestCleanupFailureRetainsSlotAndSurvivesRestart(t *testing.T) {
	s, p, owner, nb := integrationStore(t)
	ctx := context.Background()
	v := newSession(owner, nb)
	v.State = D.Stopping
	v.Reason = "requested"
	v.AgentStarted = true
	v, err := s.Start(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	env, agent := &cleanupEnvironment{}, &cleanupAgent{}
	l, err := s.Claim(ctx, "before-restart")
	if err != nil {
		t.Fatal(err)
	}
	if err = U.NewWorker(s, env, agent, nil).Step(ctx, l); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Owned(ctx, owner, v.ID)
	if err != nil || saved.State != D.Stopped || !saved.CleanupError || !saved.JournalArchived {
		t.Fatal("session must finish before cloud cleanup", saved, err)
	}
	active, err := s.Active(ctx, owner)
	if err != nil || active == nil || active.ID != v.ID {
		t.Fatal("cleanup_pending slot was released", active, err)
	}
	// Повтор возвращает прежний результат; новый Start до cleanup_confirmed запрещён.
	retry, err := s.Start(ctx, v)
	if err != nil || retry.State != D.Stopped {
		t.Fatal(retry, err)
	}
	if _, err = s.Start(ctx, newSession(owner, nb)); !errors.Is(err, D.ErrActiveSessionExists) {
		t.Fatalf("second session allowed during cleanup: %v", err)
	}
	if _, err = p.Exec(ctx, "UPDATE workspace.runtime_sessions SET next_check=CASE WHEN id=$1 THEN now() ELSE now()+interval '1 hour' END", v.ID); err != nil {
		t.Fatal(err)
	}
	l, err = s.Claim(ctx, "after-restart")
	if err != nil || l.Session.ID != v.ID {
		t.Fatal("cleanup lost after restart", l, err)
	}
	if err = U.NewWorker(s, env, agent, nil).Step(ctx, l); err != nil {
		t.Fatal(err)
	}
	if env.requests != 2 || agent.stops != 1 {
		t.Fatal("cleanup not retried independently", env.requests, agent.stops)
	}
	env.deleted = true
	if _, err = p.Exec(ctx, "UPDATE workspace.runtime_sessions SET next_check=$2 WHERE id=$1", v.ID, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	l, err = s.Claim(ctx, "confirmed")
	if err != nil {
		t.Fatal(err)
	}
	if err = U.NewWorker(s, env, agent, nil).Step(ctx, l); err != nil {
		t.Fatal(err)
	}
	saved, err = s.Owned(ctx, owner, v.ID)
	if err != nil || saved.CleanupError || saved.State != D.Stopped {
		t.Fatal(saved, err)
	}
	if active, err = s.Active(ctx, owner); err != nil || active != nil {
		t.Fatal("confirmed cleanup did not release slot", active, err)
	}
	newer, err := s.Start(ctx, newSession(owner, nb))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, "UPDATE workspace.runtime_sessions SET next_check=now()+interval '1 hour' WHERE id=$1", newer.ID); err != nil {
		t.Fatal(err)
	}
	active, err = s.Active(ctx, owner)
	if err != nil || active == nil || active.ID != newer.ID {
		t.Fatal("old cleanup affected new session", active, err)
	}
	if _, err = s.Claim(ctx, "no-cleanup-left"); !errors.Is(err, D.ErrNotFound) {
		t.Fatal(err)
	}
}
