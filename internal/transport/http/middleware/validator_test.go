package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

func TestRequireUserRejectsUnsupportedSecurityScheme(t *testing.T) {
	for _, tt := range []struct {
		name, schemeName string
		scheme           *openapi3.SecurityScheme
		allowed          bool
	}{
		{"legacy bearer", "bearerAuth", &openapi3.SecurityScheme{Type: "http", Scheme: "bearer"}, false},
		{"cookie contract", "cookieAuth", &openapi3.SecurityScheme{Type: "apiKey", In: "cookie", Name: "access_token"}, true},
		{"unknown scheme", "unreviewedAuth", &openapi3.SecurityScheme{Type: "http", Scheme: "bearer"}, false},
		{"wrong cookie", "cookieAuth", &openapi3.SecurityScheme{Type: "apiKey", In: "cookie", Name: "refresh_token"}, false},
		{"wrong transport", "cookieAuth", &openapi3.SecurityScheme{Type: "apiKey", In: "header", Name: "access_token"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/users/me", nil)
			req = req.WithContext(principal.WithUserID(req.Context(), 1))
			input := &openapi3filter.AuthenticationInput{SecuritySchemeName: tt.schemeName, SecurityScheme: tt.scheme, RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req}}
			err := requireUser(context.Background(), input)
			if (err == nil) != tt.allowed {
				t.Fatalf("allowed=%v, error=%v", tt.allowed, err)
			}
		})
	}
}
func TestContractOperationSecurityPolicy(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	public := map[string]bool{"RegisterUser": true, "LoginUser": true, "RefreshToken": true, "LogoutUser": true}
	// Logout uses refresh/CSRF and does not require a live access token.
	protected := map[string]bool{"GetCurrentUser": true, "ListNotebooks": true, "CreateNotebook": true, "GetNotebook": true, "CreateCell": true, "DeleteCell": true, "GetNotebookContent": true, "AddNotebookCell": true, "EditNotebookCell": true, "MoveNotebookCell": true, "RemoveNotebookCell": true}
	seen := 0
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			security := spec.Security
			if op.Security != nil {
				security = *op.Security
			}
			if !public[op.OperationID] && !protected[op.OperationID] {
				t.Errorf("unclassified operation %s %s (%s)", method, path, op.OperationID)
				continue
			}
			secured := len(security) > 0
			for _, requirement := range security {
				if len(requirement) == 0 {
					secured = false
				}
			}
			if secured != protected[op.OperationID] {
				t.Errorf("%s security=%v", op.OperationID, security)
			}
			seen++
		}
	}
	if seen != len(public)+len(protected) {
		t.Fatalf("classified %d operations", seen)
	}
}
