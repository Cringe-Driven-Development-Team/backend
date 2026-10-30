package httptransport

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

type allowSessions struct{}

func (allowSessions) ValidateAccess(context.Context, usecase.SessionClaims) error { return nil }
func testClaims(id int64) usecase.SessionClaims {
	return usecase.SessionClaims{UserID: id, SessionID: "test-session", Version: 1}
}
