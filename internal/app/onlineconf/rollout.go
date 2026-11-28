package onlineconf

import (
	"errors"
	"slices"
	"time"
)

var errRolloutStep = errors.New("rollout step not allowed")

// rolloutStages возвращает фиксированные ступени в порядке повышения.
func rolloutStages() [5]int {
	return [5]int{0, 10, 25, 50, 100}
}

const rolloutHold = time.Minute

// rolloutPolicy описывает следующий ручной шаг для карточки флага.
type rolloutPolicy struct {
	Stages        [5]int     `json:"stages"`
	NextPercent   *int       `json:"next_percent"`
	NextAllowedAt *time.Time `json:"next_allowed_at"`
}

// rolloutState читает состояние процентной раскатки.
func (k keyRecord) rolloutState() (bool, int, error) {
	var enabled bool
	var percent int
	if !readField(k.Data, "enabled", &enabled) ||
		!readField(k.Data, "rollout_percent", &percent) || percent < 0 || percent > 100 {
		return false, 0, errInvalidFlag
	}
	return enabled, percent, nil
}

// nextRolloutStage возвращает ближайшую ступень выше сохранённого процента.
func nextRolloutStage(percent int) *int {
	for _, stage := range rolloutStages() {
		if stage > percent {
			return &stage
		}
	}
	return nil
}

// policy возвращает срок выдержки в UTC и следующий допустимый процент.
func (k keyRecord) policy() rolloutPolicy {
	policy := rolloutPolicy{Stages: rolloutStages()}
	enabled, percent, err := k.rolloutState()
	if err != nil || !enabled {
		return policy
	}
	policy.NextPercent = nextRolloutStage(percent)
	if policy.NextPercent != nil && percent > 0 && k.RolloutStartedAt != nil {
		at := k.RolloutStartedAt.Add(rolloutHold).UTC()
		policy.NextAllowedAt = &at
	}
	return policy
}

// rolloutStart проверяет переход из current в next и возвращает начало новой выдержки.
// now — время PostgreSQL после блокировки текущей строки.
func rolloutStart(current, next keyRecord, now time.Time) (*time.Time, error) {
	wasEnabled, before, err := current.rolloutState()
	if err != nil {
		return nil, err
	}
	enabled, after, err := next.rolloutState()
	if err != nil {
		return nil, err
	}
	switch {
	case after > before:
		stage := nextRolloutStage(before)
		if !enabled || stage == nil || after != *stage ||
			(before > 0 && (!wasEnabled || current.RolloutStartedAt == nil ||
				now.Before(current.RolloutStartedAt.Add(rolloutHold)))) {
			return nil, errRolloutStep
		}
	case after < before:
		stages := rolloutStages()
		if !slices.Contains(stages[:], after) {
			return nil, errRolloutStep
		}
	}
	if !enabled || after == 0 {
		return nil, nil
	}
	if !wasEnabled || before != after || current.RolloutStartedAt == nil {
		return &now, nil
	}
	return current.RolloutStartedAt, nil
}
