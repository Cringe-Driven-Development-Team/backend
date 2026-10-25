package httptransport

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

// In-memory session fixture for the existing bootstrap/rotation/origin flows.
// It verifies state across requests, not SQL or PostgreSQL atomicity.
// Неиспользуемые методы встроенных интерфейсов не должны вызываться в этих тестах.
type csrfUsers struct {
	usecase.Usecase
	session   usecase.Session
	mutations int
	lookupErr error
}

func (u *csrfUsers) Login(context.Context, string, string) (domain.User, usecase.Session, error) {
	return domain.User{ID: 1, Login: "bob"}, u.session, nil
}
func (u *csrfUsers) Register(ctx context.Context, login, password string) (domain.User, usecase.Session, error) {
	return u.Login(ctx, login, password)
}
func (u *csrfUsers) CurrentUser(context.Context, int64) (domain.User, error) {
	return domain.User{ID: 1, Login: "bob"}, nil
}
func (u *csrfUsers) RefreshUserID(_ context.Context, refresh string) (int64, error) {
	if u.lookupErr != nil {
		return 0, u.lookupErr
	}
	if refresh != u.session.RefreshToken || refresh == "" {
		return 0, usecase.ErrInvalidRefresh
	}
	return u.session.UserID, nil
}
func (u *csrfUsers) Refresh(ctx context.Context, refresh string) (usecase.Session, error) {
	if _, err := u.RefreshUserID(ctx, refresh); err != nil {
		return usecase.Session{}, err
	}
	u.mutations++
	u.session.RefreshToken += "-rotated"
	return u.session, nil
}
func (u *csrfUsers) Logout(ctx context.Context, refresh string) error {
	if _, err := u.RefreshUserID(ctx, refresh); err != nil {
		return err
	}
	u.mutations++
	u.session.RefreshToken = ""
	return nil
}
