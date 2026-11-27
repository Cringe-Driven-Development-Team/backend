// Package workspacerpc сообщает о подтверждённой очистке зависимостей Identity.
package workspacerpc

import (
	"context"
	"errors"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/workspace/v1"
	"github.com/google/uuid"
)

// Cleanup вызывает связанное с аккаунтом durable намерение Workspace.
type Cleanup struct{ client pb.UserCleanupClient }

// NewCleanup принимает клиент с service-only mTLS полномочиями.
func NewCleanup(client pb.UserCleanupClient) *Cleanup { return &Cleanup{client: client} }

// Cleanup безопасно повторяет operation ID и требует подтверждения именно этого пользователя.
func (c *Cleanup) Cleanup(ctx context.Context, id int64, operation uuid.UUID) (bool, error) {
	out, err := c.client.BeginUserCleanup(ctx, &pb.BeginUserCleanupRequest{UserId: id, OperationId: operation.String()})
	if err != nil {
		return false, errors.New("workspace cleanup unavailable")
	}
	if out.GetUserId() != id || out.GetOperationId() != operation.String() {
		return false, errors.New("workspace cleanup response mismatch")
	}
	return out.Done && out.Stage == "done", nil
}
