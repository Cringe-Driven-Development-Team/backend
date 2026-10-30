package security

import "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"

type TokenParser interface {
	Parse(token string) (usecase.SessionClaims, error)
}
