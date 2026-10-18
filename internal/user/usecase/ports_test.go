package usecase

import (
	"context"
	"errors"
	"testing"

	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
)

type failedHasher struct{ err error }

func (h failedHasher) Hash(string) (string, error)          { return "", h.err }
func (h failedHasher) Compare(string, string) (bool, error) { return false, h.err }

type failedIssuer struct{ err error }

func (i failedIssuer) Issue(int64) (string, error) { return "", i.err }

type countingIssuer struct{ calls int }

func (i *countingIssuer) Issue(userID int64) (string, error) {
	i.calls++
	return (fakeTokens{}).Issue(userID)
}

func TestPasswordHashFailureDoesNotCreateUser(t *testing.T) {
	repo := newFakeRepo()
	failure := errors.New("hash failed")
	uc := NewUserUsecase(repo, repo, fakeTokens{}, failedHasher{failure}, usersecurity.RefreshTokens{}, refreshTTL)
	_, _, err := uc.Register(context.Background(), "alice", "password123")
	if !errors.Is(err, failure) {
		t.Fatalf("Register error = %v", err)
	}
	if len(repo.users) != 0 || len(repo.sessions) != 0 {
		t.Fatal("hash failure wrote user or session")
	}
}
func TestTokenIssueFailureDoesNotCreateSession(t *testing.T) {
	repo := newFakeRepo()
	repo.addUser("alice", "password123")
	failure := errors.New("sign failed")
	uc := NewUserUsecase(repo, repo, failedIssuer{failure}, usersecurity.PasswordHasher{}, usersecurity.RefreshTokens{}, refreshTTL)
	_, _, err := uc.Login(context.Background(), "alice", "password123")
	if !errors.Is(err, failure) {
		t.Fatalf("Login error = %v", err)
	}
	if len(repo.sessions) != 0 {
		t.Fatal("token failure created session")
	}
}

func TestPasswordCompareFailureDoesNotCreateSession(t *testing.T) {
	repo := newFakeRepo()
	repo.addUser("alice", "password123")
	failure := errors.New("password verification failed")
	tokens := &countingIssuer{}
	uc := NewUserUsecase(repo, repo, tokens, failedHasher{failure}, usersecurity.RefreshTokens{}, refreshTTL)
	_, sess, err := uc.Login(context.Background(), "alice", "password123")
	if !errors.Is(err, failure) || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login error = %v, want original verification error", err)
	}
	if sess != (Session{}) || len(repo.sessions) != 0 || tokens.calls != 0 {
		t.Fatal("password verification failure created a session")
	}
}

func TestPasswordMismatchDoesNotCreateSession(t *testing.T) {
	repo := newFakeRepo()
	repo.addUser("alice", "password123")
	tokens := &countingIssuer{}
	uc := NewUserUsecase(repo, repo, tokens, usersecurity.PasswordHasher{}, usersecurity.RefreshTokens{}, refreshTTL)
	_, sess, err := uc.Login(context.Background(), "alice", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want invalid credentials", err)
	}
	if sess != (Session{}) || len(repo.sessions) != 0 || tokens.calls != 0 {
		t.Fatal("password mismatch created a session")
	}
}
