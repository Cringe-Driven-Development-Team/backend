package mtls

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Policy разрешает точные DNS SAN только для явно зарегистрированных full method names.
type Policy map[string][]string

// Authorize проверяет удостоверенную TLS identity; отсутствие метода означает default deny.
func (p Policy) Authorize(ctx context.Context, method string) error {
	return AuthorizePeer(ctx, p[method])
}

// AuthorizePeer не использует CN/wildcard и не принимает неподтверждённый сертификат из context.
func AuthorizePeer(ctx context.Context, allowed []string) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "service authentication required")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) == 0 {
		return status.Error(codes.Unauthenticated, "service authentication required")
	}
	for _, san := range tlsInfo.State.PeerCertificates[0].DNSNames {
		for _, identity := range allowed {
			if san == identity {
				return nil
			}
		}
	}
	return status.Error(codes.PermissionDenied, "service access forbidden")
}
