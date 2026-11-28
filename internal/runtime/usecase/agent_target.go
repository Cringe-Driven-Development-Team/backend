package usecase

import (
	"context"

	"github.com/google/uuid"
)

// AgentTarget содержит внутренний адрес, идентичность VPS и монотонное назначение агенту.
type AgentTarget struct {
	SessionID, HostID uuid.UUID
	PrivateIP         string
	Fence             uint64
}

// AgentTargets разрешает сессию и сопоставляет lease Workspace с fence агента.
type AgentTargets interface {
	ResolveAgent(context.Context, uuid.UUID, uint64) (AgentTarget, error)
}

// AgentDNS возвращает имя, под которым CA удостоверяет конкретную VPS.
func AgentDNS(hostID uuid.UUID) string {
	return "agent." + hostID.String() + ".internal.cellestial.ru"
}

// AgentGRPCSettings подключает сертификаты конкретной VPS и текущий fence намерения.
type AgentGRPCSettings struct {
	MonitoringScraperIP string
	MonitoringClientCA  []byte
	IssueCertificate    func(ip, dnsName string) (cert, key []byte, err error)
	JobFence            func(context.Context, uuid.UUID) (uint64, error)
}
