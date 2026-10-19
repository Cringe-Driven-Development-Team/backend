package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
)

func TestSuccessfulUserActionsLogOnceWithoutSecrets(t *testing.T) {
	var out bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, nil))))
	t.Cleanup(func() { slog.SetDefault(old) })
	ctx := observability.WithRequestID(context.Background(), "req-user")
	repo := newFakeRepo()
	uc := NewUserUsecase(repo, repo, fakeTokens{}, usersecurity.PasswordHasher{}, usersecurity.RefreshTokens{}, refreshTTL)
	_, registered, err := uc.Register(ctx, "alice-SECRET", "password-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	_, logged, err := uc.Login(ctx, "alice-SECRET", "password-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := uc.Refresh(ctx, logged.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Logout(ctx, refreshed.RefreshToken); err != nil {
		t.Fatal(err)
	}
	_ = registered
	log := out.String()
	for _, event := range []string{"user.register", "user.login", "session.refresh", "session.logout"} {
		if strings.Count(log, `"msg":"`+event+`"`) != 1 {
			t.Fatalf("event %s: %s", event, log)
		}
	}
	if strings.Count(log, `"request_id":"req-user"`) != 4 || strings.Contains(log, "SECRET") || strings.Contains(log, logged.RefreshToken) {
		t.Fatalf("unsafe logs: %s", log)
	}
}

func TestPasswordCompareFailureHasSafeCode(t *testing.T) {
	repo := newFakeRepo()
	repo.addUser("alice", "password")
	source := errors.New("SECRET-password-hash")
	uc := NewUserUsecase(repo, repo, fakeTokens{}, failedHasher{source}, usersecurity.RefreshTokens{}, refreshTTL)
	_, _, err := uc.Login(context.Background(), "alice", "password")
	if !errors.Is(err, source) || observability.ErrorCode(err) != "password_compare_failed" {
		t.Fatalf("code=%q err=%v", observability.ErrorCode(err), err)
	}
}
