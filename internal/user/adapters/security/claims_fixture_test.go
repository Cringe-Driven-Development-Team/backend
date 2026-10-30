package security

import "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"

func testClaims(id int64) usecase.SessionClaims {
	return usecase.SessionClaims{UserID: id, SessionID: "test-session", Version: 9007199254740993}
}
