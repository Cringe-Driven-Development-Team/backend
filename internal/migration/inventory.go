package migration

import (
	"errors"
	"regexp"

	"github.com/google/uuid"
)

// Roles задаёт владельца схемы и ограниченную app-роль после переноса.
type Roles struct {
	Owner string `json:"owner"`
	App   string `json:"app"`
}

// CloudAction фиксирует внешний эффект по operation ID после остановки старых процессов.
type CloudAction struct {
	OperationID uuid.UUID `json:"operation_id"`
	Outcome     string    `json:"outcome"`
	ServerID    string    `json:"server_id,omitempty"`
	VolumeID    string    `json:"volume_id,omitempty"`
	PortID      string    `json:"port_id,omitempty"`
}

// AgentProof подтверждает сохранение journal/files и наличие совместимого агента конкретной VPS.
type AgentProof struct {
	HostID         uuid.UUID `json:"host_id"`
	Version        string    `json:"version"`
	GRPCReady      bool      `json:"grpc_ready"`
	StatePreserved bool      `json:"state_preserved"`
}

// Inventory несёт проверенный оператором внешний барьер. Сетевых секретов в нём нет.
type Inventory struct {
	Version           int               `json:"version"`
	SourceSchema      string            `json:"source_schema"`
	AdmissionStopped  bool              `json:"admission_stopped"`
	QueueDrained      bool              `json:"queue_drained"`
	ProcessesStopped  bool              `json:"processes_stopped"`
	AutomationStopped bool              `json:"automation_stopped"`
	CloudReconciled   bool              `json:"cloud_reconciled"`
	BackupVerified    bool              `json:"backup_verified"`
	Roles             map[Service]Roles `json:"roles"`
	CloudActions      []CloudAction     `json:"cloud_actions"`
	Agents            []AgentProof      `json:"agents"`
}

var roleIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// services возвращает фиксированный порядок владельцев при переносе.
func services() []Service { return []Service{Identity, Workspace, Environments} }

func (i Inventory) validate() error {
	if i.Version != 1 || i.SourceSchema != "public" {
		return errors.New("unsupported offline inventory version or source schema")
	}
	if !i.AdmissionStopped || !i.QueueDrained || !i.ProcessesStopped || !i.AutomationStopped || !i.CloudReconciled || !i.BackupVerified {
		return errors.New("offline inventory does not confirm the complete cutover barrier")
	}
	seen := map[string]bool{}
	for _, service := range services() {
		role := i.Roles[service]
		for _, name := range []string{role.Owner, role.App} {
			if !roleIdentifier.MatchString(name) || seen[name] || name == "public" || name == "postgres" {
				return errors.New("inventory must specify distinct service app and owner roles")
			}
			seen[name] = true
		}
	}
	actions := map[uuid.UUID]bool{}
	for _, action := range i.CloudActions {
		if action.OperationID == uuid.Nil || actions[action.OperationID] || (action.Outcome != "completed" && action.Outcome != "cancelled") {
			return errors.New("offline inventory contains an unresolved or duplicate cloud action")
		}
		actions[action.OperationID] = true
	}
	agents := map[uuid.UUID]bool{}
	for _, agent := range i.Agents {
		if agent.HostID == uuid.Nil || agents[agent.HostID] || agent.Version == "" {
			return errors.New("offline inventory contains an invalid or duplicate runtime agent")
		}
		agents[agent.HostID] = true
	}
	return nil
}
