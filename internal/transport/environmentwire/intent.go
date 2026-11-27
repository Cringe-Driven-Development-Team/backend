// Package environmentwire определяет общий fingerprint запроса управления окружением.
package environmentwire

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/google/uuid"
)

// OperationID выводит стабильный UUID для каждого вида намерения в пределах сессии.
func OperationID(session uuid.UUID, kind string) uuid.UUID {
	return uuid.NewSHA1(session, []byte("cellestial.environment.v1/"+kind))
}

// Fingerprint хеширует нормализованные аргументы с длинами полей, независимо от lease и request ID.
func Fingerprint(kind string, session uuid.UUID, fields ...string) []byte {
	h := sha256.New()
	for _, field := range append([]string{"cellestial.environment.v1", kind, session.String()}, fields...) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(field))
	}
	return h.Sum(nil)
}
