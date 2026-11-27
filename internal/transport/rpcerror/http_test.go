package rpcerror

import (
	"context"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRPCToHTTP проверяет полный status mapping и запрет сырых dependency errors.
func TestRPCToHTTP(t *testing.T) {
	for _, tc := range []struct {
		code     codes.Code
		kind     CallKind
		want     int
		business string
	}{
		{codes.InvalidArgument, CallBusiness, 400, "validation_error"}, {codes.Unauthenticated, CallUserSession, 401, "unauthorized"}, {codes.Unauthenticated, CallDependency, 503, "dependency_unavailable"}, {codes.PermissionDenied, CallBusiness, 403, "forbidden"}, {codes.PermissionDenied, CallDependency, 503, "dependency_unavailable"}, {codes.NotFound, CallBusiness, 404, "not_found"}, {codes.AlreadyExists, CallBusiness, 409, "conflict"}, {codes.FailedPrecondition, CallBusiness, 409, "conflict"}, {codes.Aborted, CallBusiness, 409, "conflict"}, {codes.ResourceExhausted, CallBusiness, 429, "rate_limited"}, {codes.Unavailable, CallBusiness, 503, "dependency_unavailable"}, {codes.DeadlineExceeded, CallBusiness, 504, "deadline_exceeded"}, {codes.Internal, CallBusiness, 500, "internal"}, {codes.Unknown, CallBusiness, 500, "internal"}, {codes.DataLoss, CallBusiness, 500, "internal"}, {codes.Canceled, CallBusiness, 503, "dependency_unavailable"},
	} {
		t.Run(tc.code.String()+string(tc.kind), func(t *testing.T) {
			ctx := observability.WithRequestID(context.Background(), "correlation")
			code, body := HTTP(ctx, status.Error(tc.code, "private-password"), tc.kind)
			if code != tc.want || body.Code != tc.business || body.RequestID != "correlation" || strings.Contains(body.Message, "private-password") {
				t.Fatalf("mapping: %d %+v", code, body)
			}
		})
	}
}

// TestBusinessCodes сохраняет доменные конфликты и ограничивает influence detail на HTTP status.
func TestBusinessCodes(t *testing.T) {
	for _, tc := range []struct {
		status   codes.Code
		business string
		want     int
	}{
		{codes.FailedPrecondition, "notebook_conflict", 409}, {codes.FailedPrecondition, "notebook_in_use", 409}, {codes.AlreadyExists, "active_session_exists", 409}, {codes.FailedPrecondition, "runtime_not_ready", 503}, {codes.FailedPrecondition, "workspace_closed", 410}, {codes.ResourceExhausted, "payload_too_large", 413}, {codes.Unavailable, "notebook_conflict", 503},
	} {
		code, body := HTTP(context.Background(), Error(tc.status, tc.business, nil), CallBusiness)
		if code != tc.want {
			t.Fatalf("%s=%d", tc.business, code)
		}
		if tc.status == codes.Unavailable && body.Code != "dependency_unavailable" {
			t.Fatal("untrusted business code controls response")
		}
	}
}
