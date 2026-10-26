package httptransport

import (
	"net/http"
	"testing"
	"time"

	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"go.uber.org/mock/gomock"
)

type routerFixture struct {
	handler   http.Handler
	users     *MockUserUsecase
	notebooks *MockNotebookUsecase
	access    string
}

func newRouterFixture(t *testing.T) routerFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := routerFixture{users: NewMockUserUsecase(ctrl), notebooks: NewMockNotebookUsecase(ctrl)}
	tokens := usersecurity.NewAccessToken([]byte("jwt-test-secret"), time.Hour)
	var err error
	f.access, err = tokens.Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	f.handler, err = NewRouter(Server{UserHandler: userdelivery.NewHandler(f.users), NotebookHandler: notebookdelivery.NewHandler(f.notebooks)}, tokens, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	return f
}
