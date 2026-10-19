package security

import (
	"context"
	"errors"
	"testing"
)

type refreshResolverFunc func(context.Context, string) (RefreshIdentity, error)

func (f refreshResolverFunc) ResolveRefresh(ctx context.Context, token string) (RefreshIdentity, error) {
	return f(ctx, token)
}
func TestRefreshGuardUsesResolvedOwner(t *testing.T) {
	signer := NewCSRFSigner([]byte("secret"))
	token := signer.Signed(7)
	failure := errors.New("storage unavailable")
	for _, tt := range []struct {
		name               string
		identity           RefreshIdentity
		lookupErr, wantErr error
	}{
		{name: "valid owner", identity: RefreshIdentity{UserID: 7, Valid: true}},
		{name: "other owner", identity: RefreshIdentity{UserID: 8, Valid: true}, wantErr: ErrCSRFInvalid},
		{name: "invalid session", wantErr: ErrInvalidRefresh},
		{name: "storage failure", lookupErr: failure, wantErr: failure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type key struct{}
			ctx := context.WithValue(context.Background(), key{}, "preserved")
			resolver := refreshResolverFunc(func(got context.Context, refresh string) (RefreshIdentity, error) {
				if got.Value(key{}) != "preserved" || refresh != "refresh" {
					t.Error("lookup lost context or token")
				}
				return tt.identity, tt.lookupErr
			})
			guard := NewCSRFGuard(signer, resolver)
			if err := guard.CheckRefresh(ctx, "refresh", &token); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
