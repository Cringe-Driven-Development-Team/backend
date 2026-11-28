package onlineconf

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"
	"strconv"
)

var errInvalidFlag = errors.New("invalid stored feature flag")

// flagEnabledFor возвращает решение по сохранённому флагу для ID пользователя Identity.
// Возвращает ошибку, если запись другого типа или нужные для решения поля некорректны.
func (k keyRecord) flagEnabledFor(userID int64) (bool, error) {
	var enabled bool
	if k.Type != "flag" || !readField(k.Data, "enabled", &enabled) {
		return false, errInvalidFlag
	}
	if !enabled {
		return false, nil
	}

	var excluded []userRef
	if !readField(k.Data, "exclude_users", &excluded) {
		return false, errInvalidFlag
	}
	matchesUser := func(user userRef) bool { return user.ID == userID }
	if slices.ContainsFunc(excluded, matchesUser) {
		return false, nil
	}

	var included []userRef
	if !readField(k.Data, "include_users", &included) {
		return false, errInvalidFlag
	}
	if slices.ContainsFunc(included, matchesUser) {
		return true, nil
	}

	var percent int
	if !readField(k.Data, "rollout_percent", &percent) || percent < 0 || percent > 100 {
		return false, errInvalidFlag
	}
	digest := sha256.Sum256([]byte(k.Name + "\x00" + strconv.FormatInt(userID, 10)))
	bucket := int(binary.BigEndian.Uint64(digest[:8]) % 100)
	return bucket < percent, nil
}
