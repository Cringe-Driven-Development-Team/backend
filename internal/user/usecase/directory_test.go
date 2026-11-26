package usecase

import (
	"context"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
)

// directoryStore записывает вызов, чтобы проверить границу batch до обращения к БД.
type directoryStore struct{ calls int }

func (s *directoryStore) BatchUsers(context.Context, []int64, []string) ([]domain.UserSuggestion, error) {
	s.calls++
	return []domain.UserSuggestion{{ID: 42, Login: "reader"}}, nil
}

// TestDirectoryValidation ограничивает размер и запрещает неоднозначные/пустые селекторы.
func TestDirectoryValidation(t *testing.T) {
	s := &directoryStore{}
	d := NewDirectory(s)
	for _, ids := range [][]int64{nil, {-1}, make([]int64, 101)} {
		if _, err := d.ByIDs(context.Background(), ids); err == nil {
			t.Fatal("invalid IDs accepted")
		}
	}
	for _, logins := range [][]string{nil, {""}, {" has spaces "}} {
		if _, err := d.ByLogins(context.Background(), logins); err == nil {
			t.Fatal("invalid logins accepted")
		}
	}
	if s.calls != 0 {
		t.Fatal("invalid batch reached store")
	}
	users, err := d.ByIDs(context.Background(), []int64{42, 43})
	if err != nil || len(users) != 1 || s.calls != 1 {
		t.Fatalf("batch: %v %v", users, err)
	}
}
