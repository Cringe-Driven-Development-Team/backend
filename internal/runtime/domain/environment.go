// Package domain содержит модель окружения (VPS) пользовательской сессии и правила переходов между состояниями.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// State — состояние окружения.
type State string

const (
	StateQueued         State = "QUEUED"
	StateCreating       State = "CREATING"
	StateConfiguring    State = "CONFIGURING"
	StateVerifying      State = "VERIFYING"
	StateReady          State = "READY"
	StateDeleting       State = "DELETING"
	StateDeleted        State = "DELETED"
	StateCleanupPending State = "CLEANUP_PENDING"
	StateFailed         State = "FAILED"
)

// Resources — идентификаторы облачных ресурсов окружения.
type Resources struct {
	ServerID  string
	VolumeID  string
	PortID    string
	PrivateIP string
}

// Environment — окружение пользовательской сессии.
type Environment struct {
	SessionID, OperationID   uuid.UUID
	Language, Profile        string
	State, DesiredState      State
	Resources                Resources
	Endpoint, AgentVersion   string
	LastError, CleanupResult string
	Version                  int64
	CreatedAt, UpdatedAt     time.Time
}

var transitions = map[State]map[State]struct{}{
	StateQueued:         {StateCreating: {}, StateDeleting: {}},
	StateCreating:       {StateConfiguring: {}, StateDeleting: {}, StateCleanupPending: {}},
	StateConfiguring:    {StateVerifying: {}, StateDeleting: {}, StateCleanupPending: {}},
	StateVerifying:      {StateReady: {}, StateDeleting: {}, StateCleanupPending: {}},
	StateReady:          {StateDeleting: {}},
	StateDeleting:       {StateDeleted: {}, StateCleanupPending: {}},
	StateCleanupPending: {StateDeleting: {}, StateFailed: {}},
}

// CanTransition сообщает, допустим ли переход from → to.
func CanTransition(from, to State) bool {
	_, ok := transitions[from][to]
	return ok
}

// Finished сообщает, что состояние конечное.
func (s State) Finished() bool { return s == StateDeleted || s == StateFailed }

// ValidLanguage проверяет язык ядра.
func ValidLanguage(l string) bool { return l == "python" || l == "r" }
