package usecase

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"
)

// fakeRepo stores users and sessions for token rotation/logout and logging flows.
// This fixture does not verify PostgreSQL behavior.
type fakeRepo struct {
	mu       sync.Mutex
	users    map[int64]fakeUser
	sessions map[string]fakeSession
	// lastID растёт с каждым пользователем, как IDENTITY в Postgres.
	lastID int64
}

type fakeUser struct {
	domain.User
	hash string
}

type fakeSession struct {
	userID    int64
	expiresAt time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{users: map[int64]fakeUser{}, sessions: map[string]fakeSession{}}
}

// addUser кладёт пользователя с паролем. MinCost, чтобы тесты не ждали bcrypt.
func (r *fakeRepo) addUser(login, password string) domain.User {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	r.lastID++
	u := domain.User{ID: r.lastID, Login: login}
	r.users[u.ID] = fakeUser{User: u, hash: string(hash)}
	return u
}

func (r *fakeRepo) CreateUser(_ context.Context, login, passwordHash string) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if strings.EqualFold(u.Login, login) {
			return domain.User{}, ErrLoginTaken
		}
	}
	r.lastID++
	u := domain.User{ID: r.lastID, Login: login}
	r.users[u.ID] = fakeUser{User: u, hash: passwordHash}
	return u, nil
}

func (r *fakeRepo) UserByLogin(_ context.Context, login string) (domain.User, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if strings.EqualFold(u.Login, login) {
			return u.User, u.hash, nil
		}
	}
	return domain.User{}, "", ErrUserNotFound
}

func (r *fakeRepo) UserByID(_ context.Context, id int64) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.User{}, ErrUserNotFound
	}
	return u.User, nil
}

func (r *fakeRepo) CreateSession(_ context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[tokenHash] = fakeSession{userID: userID, expiresAt: expiresAt}
	return nil
}

func (r *fakeRepo) DeleteSession(_ context.Context, tokenHash string) (int64, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[tokenHash]
	if !ok {
		return 0, time.Time{}, ErrSessionNotFound
	}
	delete(r.sessions, tokenHash)
	return s.userID, s.expiresAt, nil
}

func flowIssuer(t *testing.T, calls int) *MockTokenIssuer {
	t.Helper()
	issuer := NewMockTokenIssuer(gomock.NewController(t))
	issuer.EXPECT().Issue(int64(1)).Return("access-1", nil).Times(calls)
	return issuer
}

func (r *fakeRepo) SessionByTokenHash(_ context.Context, tokenHash string) (int64, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[tokenHash]
	if !ok {
		return 0, time.Time{}, ErrSessionNotFound
	}
	return s.userID, s.expiresAt, nil
}
