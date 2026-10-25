package httptransport

import (
	"net/http"
	"testing"
	"time"

	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"go.uber.org/mock/gomock"
)

type routerFixture struct {
	handler         http.Handler
	csrf            *security.CSRFSigner
	users           *MockUserUsecase
	notebooks       *MockNotebookUsecase
	access, expired string
}

func newRouterFixture(t *testing.T) routerFixture {
	return newRouterFixtureOrigins(t, []string{"https://cellestial.ru"}, middleware.DefaultAppOrigin)
}
func newRouterFixtureOrigins(t *testing.T, origins []string, appOrigin string) routerFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := routerFixture{users: NewMockUserUsecase(ctrl), notebooks: NewMockNotebookUsecase(ctrl), csrf: security.NewCSRFSigner([]byte("csrf-test-secret"))}
	tokens := usersecurity.NewAccessToken([]byte("jwt-test-secret"), time.Hour)
	var err error
	f.access, err = tokens.Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	f.expired, err = usersecurity.NewAccessToken([]byte("jwt-test-secret"), -time.Hour).Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	f.handler, err = NewRouter(Server{UserHandler: userdelivery.NewHandler(f.users, cookies.Config{AccessPath: BaseURL, RefreshPath: BaseURL + "/auth", AccessTTL: time.Hour, CSRFTTL: 30 * 24 * time.Hour, Secure: true}, f.csrf), NotebookHandler: notebookdelivery.NewHandler(f.notebooks)}, tokens, origins, f.csrf, 30*24*time.Hour, userdelivery.NewRefreshResolver(f.users), appOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Explicit expectations for one successful request in the existing origin tests.
func expectSessionRequest(f routerFixture, operation string, lookup bool) {
	session := usecase.Session{UserID: 1, AccessToken: f.access, RefreshToken: "rotated-refresh", ExpiresAt: time.Now().Add(time.Hour)}
	if lookup {
		f.users.EXPECT().RefreshUserID(gomock.Any(), "refresh").Return(int64(1), nil)
	}
	switch operation {
	case "login":
		f.users.EXPECT().Login(gomock.Any(), "bob", "password123").Return(domain.User{ID: 1, Login: "bob"}, session, nil)
	case "register":
		f.users.EXPECT().Register(gomock.Any(), "bob", "password123").Return(domain.User{ID: 1, Login: "bob"}, session, nil)
	case "refresh":
		f.users.EXPECT().Refresh(gomock.Any(), "refresh").Return(session, nil)
	}
}
