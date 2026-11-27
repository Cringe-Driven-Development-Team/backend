package rpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestMonitoringSeparatesRPCSides проверяет общий контракт клиентских и серверных RPC.
func TestMonitoringSeparatesRPCSides(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewMetrics(registry)
	metrics.begin("/cellestial.test.v1.Service/Watch", "stream", "server")("Canceled")
	metrics.begin("/cellestial.test.v1.Service/Watch", "stream", "client")("DeadlineExceeded")
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "cellestial_grpc_calls_total" && len(family.Metric) == 2 {
			return
		}
	}
	t.Fatal("expected two calls series")
}

// TestUnknownRPC проверяет реальное отклонение grpc-go и ограниченные labels.
func TestUnknownRPC(t *testing.T) {
	reg := prometheus.NewRegistry()
	server := grpc.NewServer(ServerOptions(UnaryConfig{Metrics: NewMetrics(reg)})...)
	listener := bufconn.Listen(64 << 10)
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, method := range []string{"/private.Service/One", "/private.Service/Two"} {
		if err = conn.Invoke(ctx, method, &emptypb.Empty{}, &emptypb.Empty{}); status.Code(err) != codes.Unimplemented {
			t.Fatal(err)
		}
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 1 || len(families[0].Metric) != 1 || families[0].Metric[0].GetCounter().GetValue() != 2 {
		t.Fatal("unknown RPCs should form one counter series")
	}
}
