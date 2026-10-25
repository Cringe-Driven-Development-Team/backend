package security

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"
)

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
			resolver := NewMockRefreshUserResolver(gomock.NewController(t))
			resolver.EXPECT().ResolveRefresh(ctx, "refresh").Return(tt.identity, tt.lookupErr)
			guard := NewCSRFGuard(signer, resolver)
			if err := guard.CheckRefresh(ctx, "refresh", &token); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
