package rpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// testStream позволяет проверить обёртку context и metadata без сокрытия транспортной цепочки.
type testStream struct {
	ctx    context.Context
	header metadata.MD
}

func (s *testStream) Context() context.Context { return s.ctx }
func (s *testStream) SetHeader(md metadata.MD) error {
	s.header = metadata.Join(s.header, md)
	return nil
}
func (s *testStream) SendHeader(md metadata.MD) error { return s.SetHeader(md) }
func (s *testStream) SetTrailer(metadata.MD)          {}
func (s *testStream) SendMsg(any) error               { return nil }
func (s *testStream) RecvMsg(any) error               { return io.EOF }

// TestStreamTransport проверяет итоговый лог, auth, recovery и передачу context обработчику.
func TestStreamTransport(t *testing.T) {
	for _, tc := range []struct {
		name   string
		peer   string
		result codes.Code
		panic  bool
	}{
		{name: "success", peer: "workspace", result: codes.OK}, {name: "auth rejected", peer: "bff", result: codes.PermissionDenied}, {name: "validation", peer: "workspace", result: codes.InvalidArgument}, {name: "panic", peer: "workspace", result: codes.Internal, panic: true}, {name: "dependency", peer: "workspace", result: codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			cfg := UnaryConfig{Service: "identity", Version: "test", Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Timeout: time.Second, AllowedPeers: map[string][]string{"/service/Stream": {"workspace"}}}
			cert := &x509.Certificate{DNSNames: []string{tc.peer}}
			ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}, PeerCertificates: []*x509.Certificate{cert}}}})
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-request-id", "stream-correlation"))
			stream := &testStream{ctx: ctx}
			called := false
			err := StreamServer(cfg)(nil, stream, &grpc.StreamServerInfo{FullMethod: "/service/Stream"}, func(_ any, s grpc.ServerStream) error {
				called = true
				if observability.RequestID(s.Context()) != "stream-correlation" {
					t.Error("correlation lost")
				}
				if _, ok := s.Context().Deadline(); !ok {
					t.Error("missing stream deadline")
				}
				if tc.panic {
					panic("secret-panic")
				}
				if tc.result == codes.OK {
					return nil
				}
				return status.Error(tc.result, "safe error")
			})
			if status.Code(err) != tc.result {
				t.Fatalf("status=%v want=%v", err, tc.result)
			}
			if (tc.peer == "workspace") != called {
				t.Fatal("wrong handler invocation")
			}
			if got := stream.header.Get("x-request-id"); len(got) != 1 || got[0] != "stream-correlation" {
				t.Fatal("response correlation lost")
			}
			if strings.Count(logs.String(), `"msg":"rpc.request"`) != 1 || strings.Contains(logs.String(), "secret-panic") {
				t.Fatal("unsafe or duplicate final log")
			}
		})
	}
}

// TestRPCMetricsMessages учитывает bytes/messages без labels из payload или correlation.
func TestRPCMetricsMessages(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	stream := contextStream{ServerStream: &testStream{ctx: context.Background()}, ctx: context.Background(), metrics: m, method: "/service/Stream"}
	if err := stream.SendMsg(wrapperspb.String("private-payload")); err != nil {
		t.Fatal(err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range families {
		if f.GetName() == "cellestial_grpc_stream_messages_total" {
			found = true
			if len(f.Metric) != 1 || f.Metric[0].Counter.GetValue() != 1 {
				t.Fatal("incorrect message count")
			}
		}
		if strings.Contains(f.String(), "private-payload") {
			t.Fatal("payload in metrics")
		}
	}
	if !found {
		t.Fatal("message metrics missing")
	}
}
