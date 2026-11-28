package onlineconf

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"
	"strconv"
)

var errInvalidFlag = errors.New("invalid stored feature flag")

type flagReason string

const (
	flagDisabled flagReason = "disabled"
	flagExcluded flagReason = "excluded"
	flagIncluded flagReason = "included"
	flagRollout  flagReason = "rollout"
)

type flagDecision struct {
	Enabled bool       `json:"enabled"`
	Reason  flagReason `json:"reason"`
}

// flagEnabledFor возвращает решение по сохранённому флагу для ID пользователя Identity.
// Возвращает ошибку, если запись другого типа или нужные для решения поля некорректны.
func (k keyRecord) flagEnabledFor(userID int64) (bool, error) {
	decision, err := k.evaluateFlag(userID)
	return decision.Enabled, err
}

// evaluateFlag вычисляет состояние и причину по первому сработавшему правилу.
func (k keyRecord) evaluateFlag(userID int64) (flagDecision, error) {
	var enabled bool
	if k.Type != "flag" || !readField(k.Data, "enabled", &enabled) {
		return flagDecision{}, errInvalidFlag
	}
	if !enabled {
		return flagDecision{Reason: flagDisabled}, nil
	}

	var excluded []userRef
	if !readField(k.Data, "exclude_users", &excluded) {
		return flagDecision{}, errInvalidFlag
	}
	matchesUser := func(user userRef) bool { return user.ID == userID }
	if slices.ContainsFunc(excluded, matchesUser) {
		return flagDecision{Reason: flagExcluded}, nil
	}

	var included []userRef
	if !readField(k.Data, "include_users", &included) {
		return flagDecision{}, errInvalidFlag
	}
	if slices.ContainsFunc(included, matchesUser) {
		return flagDecision{Enabled: true, Reason: flagIncluded}, nil
	}

	var percent int
	if !readField(k.Data, "rollout_percent", &percent) || percent < 0 || percent > 100 {
		return flagDecision{}, errInvalidFlag
	}
	digest := sha256.Sum256([]byte(k.Name + "\x00" + strconv.FormatInt(userID, 10)))
	bucket := int(binary.BigEndian.Uint64(digest[:8]) % 100)
	return flagDecision{Enabled: bucket < percent, Reason: flagRollout}, nil
}
