package usecase

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/google/uuid"
)

// Intent содержит неизменный ключ запроса и проверяемый lease вызывающего Workspace.
type Intent struct {
	OperationID uuid.UUID
	SessionID   uuid.UUID
	Kind        JobKind
	Fingerprint []byte
	Fence       int64
}

// IntentRepository сохраняет намерение и задание атомарно.
type IntentRepository interface {
	ApplyIntent(context.Context, Intent, domain.Environment, int, string) (domain.Environment, error)
	ReadIntent(context.Context, uuid.UUID) (Intent, domain.Environment, error)
}

// IntentService валидирует вход управления окружениями и делегирует атомарную запись.
type IntentService struct {
	repo     IntentRepository
	env      EnvironmentService
	settings Settings
}

// NewIntentService подключает хранилище намерений к конфигурации provisioning.
func NewIntentService(repo IntentRepository, env EnvironmentService, settings Settings) *IntentService {
	return &IntentService{repo: repo, env: env, settings: settings}
}

// Create ставит provisioning в очередь с предоставленным operation ID.
func (s *IntentService) Create(ctx context.Context, i Intent, language, profile string) (domain.Environment, error) {
	if !domain.ValidLanguage(language) {
		return domain.Environment{}, ErrInvalidLanguage
	}
	profile = normProfile(profile)
	if !s.settings.Profiles[profile] {
		return domain.Environment{}, ErrUnknownProfile
	}
	init := domain.Environment{SessionID: i.SessionID, OperationID: i.OperationID, Language: language,
		Profile: profile, State: domain.StateQueued, DesiredState: domain.StateReady, AgentVersion: s.settings.AgentVersion}
	return s.repo.ApplyIntent(ctx, i, init, s.settings.MaxActive, "")
}

// Delete ставит очистку в очередь, даже когда запрос создания ещё не пришёл.
func (s *IntentService) Delete(ctx context.Context, i Intent, reason string) (domain.Environment, error) {
	tombstone := domain.Environment{SessionID: i.SessionID, OperationID: i.OperationID, Language: "python",
		Profile: defaultProfile, State: domain.StateDeleting, DesiredState: domain.StateDeleted, AgentVersion: s.settings.AgentVersion}
	return s.repo.ApplyIntent(ctx, i, tombstone, 0, reason)
}

// Get возвращает текущее состояние окружения.
func (s *IntentService) Get(ctx context.Context, id uuid.UUID) (domain.Environment, error) {
	return s.env.Get(ctx, id)
}

// Operation читает durable запись и фактическое состояние окружения.
func (s *IntentService) Operation(ctx context.Context, id uuid.UUID) (Intent, domain.Environment, error) {
	return s.repo.ReadIntent(ctx, id)
}
