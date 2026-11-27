package grpcdelivery_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"testing"
	"time"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	delivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/grpc"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type blockedDeletion struct {
	delivery.UserDeletionService
	contextError error
}

// Begin имитирует отменённое ожидание БД с обёрнутой ошибкой контекста.
func (d blockedDeletion) Begin(ctx context.Context, _ int64, _ uuid.UUID) (usecase.Deletion, error) {
	err := d.contextError
	if err == nil {
		<-ctx.Done()
		err = ctx.Err()
	}
	return usecase.Deletion{}, fmt.Errorf("waiting for DB: %w", err)
}

// TestDeletionRPCContextMetrics проверяет код ответа и счётчик через настоящий gRPC.
func TestDeletionRPCContextMetrics(t *testing.T) {
	for _, test := range []struct {
		code         codes.Code
		contextError error
	}{{codes.DeadlineExceeded, nil}, {codes.Canceled, context.Canceled}} {
		t.Run(test.code.String(), func(t *testing.T) {
			pki := newPKI(t)
			reg := prometheus.NewRegistry()
			opts := rpc.ServerOptions(rpc.UnaryConfig{Timeout: 30 * time.Millisecond, Metrics: rpc.NewMetrics(reg),
				AllowedPeers: map[string][]string{pb.UserAdministration_RequestUserDeletion_FullMethodName: {"identity-admin.internal.cellestial.ru"}},
			})
			server := grpc.NewServer(append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{
				MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert,
				ClientCAs: pki.pool, Certificates: []tls.Certificate{pki.certificate(t, "identity.internal.cellestial.ru", x509.ExtKeyUsageServerAuth)},
			})))...)
			pb.RegisterUserAdministrationServer(server, delivery.NewUserAdministration(blockedDeletion{contextError: test.contextError}))
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			client := pb.NewUserAdministrationClient(dialProcess(t, pki, listener.Addr().String(), "identity", "identity-admin"))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = client.RequestUserDeletion(ctx, &pb.RequestUserDeletionRequest{UserId: 1, OperationId: uuid.NewString()})
			if status.Code(err) != test.code {
				t.Fatalf("RPC result = %v, want %v", err, test.code)
			}
			families, err := reg.Gather()
			if err != nil {
				t.Fatal(err)
			}
			for _, family := range families {
				if family.GetName() != "cellestial_grpc_calls_total" || len(family.Metric) != 1 || family.Metric[0].GetCounter().GetValue() != 1 {
					continue
				}
				for _, label := range family.Metric[0].Label {
					if label.GetName() == "grpc_code" && label.GetValue() == test.code.String() {
						return
					}
				}
			}
			t.Fatal("expected one RPC counter sample with correct context status")
		})
	}
}
