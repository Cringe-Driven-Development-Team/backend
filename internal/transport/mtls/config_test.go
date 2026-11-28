package mtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// TestFailClosedTLS не допускает plaintext и непроверенное server name.
func TestFailClosedTLS(t *testing.T) {
	if _, err := ServerConfig(Config{}); err == nil {
		t.Fatal("missing credentials accepted")
	}
	if _, err := ClientConfig(Config{}, ""); err == nil {
		t.Fatal("missing server name accepted")
	}
}

// TestPeerPolicy разрешает только verified peer и точный DNS SAN конкретного метода.
func TestPeerPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verified bool
		san      string
		want     codes.Code
	}{{"correct", true, "workspace.internal.cellestial.ru", codes.OK}, {"wrong service", true, "bff.internal.cellestial.ru", codes.PermissionDenied}, {"wildcard", true, "*.internal.cellestial.ru", codes.PermissionDenied}, {"unverified", false, "workspace.internal.cellestial.ru", codes.Unauthenticated}, {"CN fallback forbidden", true, "", codes.PermissionDenied}} {
		t.Run(tc.name, func(t *testing.T) {
			cert := &x509.Certificate{DNSNames: []string{tc.san}}
			state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
			if tc.verified {
				state.VerifiedChains = [][]*x509.Certificate{{cert}}
			}
			ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: state}})
			p := Policy{"/identity/Validate": {"workspace.internal.cellestial.ru"}}
			if code := status.Code(p.Authorize(ctx, "/identity/Validate")); code != tc.want {
				t.Fatalf("status=%v", code)
			}
			if code := status.Code(p.Authorize(ctx, "/unknown")); code == codes.OK {
				t.Fatal("unregistered method allowed")
			}
		})
	}
}

func TestHTTPClientNames(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"workspace.internal.cellestial.ru"}}
	verified := tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}, PeerCertificates: []*x509.Certificate{cert}}
	allowed := []string{"bff.internal.cellestial.ru", "workspace.internal.cellestial.ru"}
	if err := verifyAnyDNSName(verified, allowed); err != nil {
		t.Fatalf("allowed client rejected: %v", err)
	}
	if verifyAnyDNSName(verified, allowed[:1]) == nil {
		t.Fatal("client outside the list accepted")
	}
	if verifyAnyDNSName(tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}, allowed) == nil {
		t.Fatal("unverified chain accepted")
	}
	if verifyAnyDNSName(verified, nil) == nil {
		t.Fatal("empty list accepted a client")
	}
}
