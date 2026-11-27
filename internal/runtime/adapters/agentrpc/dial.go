// Package agentrpc устанавливает mTLS соединение Environments с конкретной VPS.
package agentrpc

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"time"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	filesv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Dialer настраивает закрытый transport. Каждый вызов владеет соединением до EOF или отмены.
type Dialer struct {
	base   *tls.Config
	port   string
	unary  grpc.UnaryClientInterceptor
	stream grpc.StreamClientInterceptor
}

// NewDialer загружает CA и удостоверение Environments единожды.
func NewDialer(tls mtls.Config, port int, logger *slog.Logger, version string, metrics *rpc.Metrics) (*Dialer, error) {
	if port <= 0 || port > 65535 {
		return nil, errors.New("invalid agent gRPC port")
	}
	config, err := mtls.ClientConfig(tls, "agent.internal.cellestial.ru")
	if err != nil {
		return nil, err
	}
	policies := map[string]rpc.MethodPolicy{}
	for _, method := range []string{
		agentv1.Control_Assign_FullMethodName, agentv1.Control_Health_FullMethodName,
		agentv1.Control_Execute_FullMethodName, agentv1.Control_GetExecution_FullMethodName,
		agentv1.Control_ReadEvents_FullMethodName, agentv1.Control_InspectVariables_FullMethodName,
		agentv1.Control_Activity_FullMethodName, agentv1.Control_Drain_FullMethodName, agentv1.Control_Stop_FullMethodName,
		agentv1.Control_Interrupt_FullMethodName, agentv1.Control_Restart_FullMethodName, agentv1.Control_StopExecutions_FullMethodName,
		filesv1.Files_Check_FullMethodName, filesv1.Files_OpenSocket_FullMethodName, filesv1.Files_CloseSocket_FullMethodName,
		filesv1.Files_Command_FullMethodName, filesv1.Files_Subscribe_FullMethodName, filesv1.Files_Upload_FullMethodName, filesv1.Files_Download_FullMethodName,
	} {
		policies[method] = rpc.MethodPolicy{}
	}
	policies[agentv1.Control_Health_FullMethodName] = rpc.MethodPolicy{Retry: true}
	policies[agentv1.Control_Assign_FullMethodName] = rpc.MethodPolicy{Timeout: time.Minute}
	c := rpc.ClientConfig{Logger: logger, Service: "environments", Version: version, Timeout: 5 * time.Second, Methods: policies, Metrics: metrics}
	return &Dialer{base: config, port: strconv.Itoa(port), unary: rpc.UnaryClient(c), stream: rpc.StreamClient(c)}, nil
}

// Dial использует IP из достоверного состояния Environments и точный SAN из идентичности VPS.
func (d *Dialer) Dial(target usecase.AgentTarget) (*grpc.ClientConn, error) {
	ip, err := netip.ParseAddr(target.PrivateIP)
	if err != nil || target.HostID == uuid.Nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" {
		return nil, errors.New("agent address unavailable")
	}
	name := usecase.AgentDNS(target.HostID)
	creds := credentials.NewTLS(mtls.ForServer(d.base, name))
	return grpc.NewClient("passthrough:///"+net.JoinHostPort(ip.String(), d.port),
		grpc.WithAuthority(name), grpc.WithTransportCredentials(creds), grpc.WithDisableRetry(),
		grpc.WithMaxHeaderListSize(16<<10), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8<<20), grpc.MaxCallSendMsgSize(8<<20)),
		grpc.WithUnaryInterceptor(d.unary), grpc.WithStreamInterceptor(d.stream))
}
