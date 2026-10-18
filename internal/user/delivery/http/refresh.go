package delivery

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

type RefreshResolver struct{ uc usecase.Usecase }

// NewRefreshResolver адаптирует прикладной результат, скрывая ошибки usecase от общей HTTP-защиты.
func NewRefreshResolver(uc usecase.Usecase) *RefreshResolver {
	return &RefreshResolver{uc: uc}
}
func (r *RefreshResolver) ResolveRefresh(ctx context.Context, token string) (security.RefreshIdentity, error) {
	id, err := r.uc.RefreshUserID(ctx, token)
	if errors.Is(err, usecase.ErrInvalidRefresh) {
		return security.RefreshIdentity{}, nil
	}
	if err != nil {
		return security.RefreshIdentity{}, err
	}
	return security.RefreshIdentity{UserID: id, Valid: true}, nil
}
