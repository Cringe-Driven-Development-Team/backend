// Identity-admin принимает явный UUID операции удаления и использует выделенный mTLS сертификат.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// main печатает только безопасный результат CLI без содержимого credentials.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "identity-admin"))
	if err := run(os.Args[1:]); err != nil {
		slog.Error("identity_admin.failed", "error", err.Error())
		os.Exit(1)
	}
}

// run принимает delete <user_id> <operation_uuid> либо status <operation_uuid>.
func run(args []string) error {
	deleteCommand := len(args) == 3 && args[0] == "delete"
	statusCommand := len(args) == 2 && args[0] == "status"
	if !deleteCommand && !statusCommand {
		return errors.New("usage: identity-admin delete <user_id> <operation_uuid> | status <operation_uuid>")
	}
	operation, err := uuid.Parse(args[len(args)-1])
	if err != nil || operation == uuid.Nil {
		return errors.New("invalid operation ID")
	}
	var uid int64
	if args[0] == "delete" {
		uid, err = strconv.ParseInt(args[1], 10, 64)
		if err != nil || uid <= 0 {
			return errors.New("invalid user ID")
		}
	}
	addr := os.Getenv("IDENTITY_GRPC_ADDR")
	if addr == "" {
		return errors.New("IDENTITY_GRPC_ADDR is required")
	}
	options, err := rpc.ClientOptions(rpc.ClientConfig{TLS: mtls.Config{
		CertFile: os.Getenv("TLS_CERT_FILE"), KeyFile: os.Getenv("TLS_KEY_FILE"), CAFile: os.Getenv("TLS_CA_FILE")},
		ServerName: "identity.internal.cellestial.ru", Service: "identity-admin", Version: "local", Timeout: 5 * time.Second,
		Methods: map[string]rpc.MethodPolicy{
			pb.UserAdministration_RequestUserDeletion_FullMethodName: {Retry: true},
			pb.UserAdministration_GetUserDeletion_FullMethodName:     {Retry: true},
		},
	})
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, options...)
	if err != nil {
		return errors.New("invalid Identity gRPC address")
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	api := pb.NewUserAdministrationClient(conn)
	var reply *pb.UserDeletion
	if uid > 0 {
		reply, err = api.RequestUserDeletion(ctx, &pb.RequestUserDeletionRequest{UserId: uid, OperationId: operation.String()})
	} else {
		reply, err = api.GetUserDeletion(ctx, &pb.GetUserDeletionRequest{OperationId: operation.String()})
	}
	if err != nil {
		return errors.New("Identity RPC failed: " + status.Code(err).String())
	}
	return json.NewEncoder(os.Stdout).Encode(reply)
}
