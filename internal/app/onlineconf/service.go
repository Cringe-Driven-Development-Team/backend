package onlineconf

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	rpctransport "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	"google.golang.org/grpc"
)

// Run читает настройки процесса из окружения и обслуживает HTTP до отмены ctx.
func Run(ctx context.Context, logger *slog.Logger) error {
	bffKey := os.Getenv("BFF_API_KEY")
	if strings.TrimSpace(bffKey) == "" {
		return errors.New("missing BFF_API_KEY")
	}
	addr := os.Getenv("ONLINECONF_HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8085"
	}
	target := os.Getenv("IDENTITY_GRPC_ADDR")
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" || port == "" {
		return errors.New("invalid IDENTITY_GRPC_ADDR")
	}
	options, err := rpctransport.ClientOptions(rpctransport.ClientConfig{
		TLS: mtls.Config{
			CertFile: os.Getenv("TLS_CERT_FILE"),
			KeyFile:  os.Getenv("TLS_KEY_FILE"),
			CAFile:   os.Getenv("TLS_CA_FILE"),
		},
		ServerName: "identity.internal.cellestial.ru",
		Service:    "onlineconf",
		Version:    os.Getenv("VERSION"),
		Logger:     logger,
		Timeout:    2 * time.Second,
		Methods:    map[string]rpctransport.MethodPolicy{identityv1.OnlineConfAccess_Authorize_FullMethodName: {}},
	})
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(target, options...)
	if err != nil {
		return errors.New("cannot initialize Identity gRPC client")
	}
	defer conn.Close()

	admin := http.NewServeMux()
	admin.HandleFunc("GET /admin/access", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{
		Handler:           newRouter(identityv1.NewOnlineConfAccessClient(conn), admin, bffKey),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.New("cannot listen on OnlineConf HTTP address")
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case <-serveErr:
		return errors.New("OnlineConf HTTP listener failed")
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = server.Shutdown(shutdown)
	if err != nil {
		_ = server.Close()
	}
	<-serveErr
	return err
}
