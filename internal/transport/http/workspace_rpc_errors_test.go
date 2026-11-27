package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	filesv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/adapters/environmentsrpc"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/adapters/identityrpc"
	runtimedelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failingFiles struct {
	filesv1.FilesClient
	code codes.Code
}

func (f failingFiles) Check(context.Context, *filesv1.CheckRequest, ...grpc.CallOption) (*filesv1.CheckResponse, error) {
	return nil, status.Error(f.code, "private-detail")
}

type failingIdentity struct {
	identityv1.SessionClient
	code codes.Code
}

func (f failingIdentity) ValidateSession(context.Context, *identityv1.ValidateSessionRequest, ...grpc.CallOption) (*identityv1.ValidateSessionResponse, error) {
	return nil, status.Error(f.code, "private-detail")
}

type readyFileSession struct{ usecase.Store }

func (readyFileSession) Owned(context.Context, int64, uuid.UUID) (domain.Session, error) {
	return domain.Session{State: domain.Ready}, nil
}

// TestWorkspaceRPCErrorResponses проверяет рабочие адаптеры, correlation и HTTP-ответ BFF.
func TestWorkspaceRPCErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		dependency string
		rpc        codes.Code
		http       int
		code       string
	}{
		{"identity", codes.Unauthenticated, 401, "unauthorized"},
		{"identity", codes.PermissionDenied, 503, "dependency_unavailable"},
		{"identity", codes.Internal, 500, "internal"},
		{"identity", codes.DeadlineExceeded, 504, "deadline_exceeded"},
		{"files", codes.InvalidArgument, 400, "validation_error"},
		{"files", codes.PermissionDenied, 503, "runtime_not_ready"},
		{"files", codes.NotFound, 404, "not_found"},
		{"files", codes.AlreadyExists, 409, "conflict"},
		{"files", codes.FailedPrecondition, 409, "conflict"},
		{"files", codes.Aborted, 409, "conflict"},
		{"files", codes.ResourceExhausted, 429, "rate_limited"},
		{"files", codes.Unavailable, 503, "runtime_not_ready"},
		{"files", codes.DeadlineExceeded, 504, "deadline_exceeded"},
		{"files", codes.Internal, 500, "internal"},
	} {
		t.Run(tc.dependency+"/"+tc.rpc.String(), func(t *testing.T) {
			var verifier authn.SessionVerifier = routerVerifier(func(context.Context, string) (authn.Principal, error) {
				return authn.Principal{UserID: 7}, nil
			})
			if tc.dependency == "identity" {
				verifier = identityrpc.NewSession(failingIdentity{code: tc.rpc})
			}
			files := usecase.NewWorkspaceFiles(readyFileSession{}, environmentsrpc.NewFiles(failingFiles{code: tc.rpc}))
			router, err := NewWorkspaceRouter(WorkspaceHandlers{
				NotebookHandler: &NotebookHandler{},
				RuntimeHandler:  runtimedelivery.NewHandler(nil),
				Files:           runtimedelivery.NewWorkspaceFileHandler(files),
			}, verifier, "test-bff", nil)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", BaseURL+"/runtime-sessions/"+uuid.NewString()+"/workspace/check", nil)
			r.Header.Set("X-BFF-Key", "test-bff")
			r.Header.Set("Authorization", "Bearer test-token")
			r.Header.Set("X-Request-ID", "workspace-rpc-error")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			var body struct {
				Code      string `json:"code"`
				RequestID string `json:"request_id"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.http || body.Code != tc.code || body.RequestID != "workspace-rpc-error" || strings.Contains(w.Body.String(), "private-detail") {
				t.Fatalf("status=%d body=%s, want %d %s", w.Code, w.Body, tc.http, tc.code)
			}
		})
	}
}
