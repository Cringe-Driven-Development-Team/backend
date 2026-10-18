// Package security реализует криптографические зависимости сценариев пользователя.
package security

import "golang.org/x/crypto/bcrypt"

// PasswordHasher хранит пароли с текущей стоимостью bcrypt.
type PasswordHasher struct{}

func (PasswordHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
func (PasswordHasher) Compare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
