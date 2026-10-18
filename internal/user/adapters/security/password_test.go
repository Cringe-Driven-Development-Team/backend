package security

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHasherCompare(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	futureHash := "$9" + string(hash[2:])
	for _, tt := range []struct {
		name, hash, password string
		wantMatch            bool
		wantErr              error
	}{
		{"match", string(hash), "password123", true, nil},
		{"mismatch", string(hash), "wrong-password", false, nil},
		{"damaged hash", "broken", "password123", false, bcrypt.ErrHashTooShort},
		{"unsupported version", futureHash, "password123", false, bcrypt.HashVersionTooNewError('9')},
	} {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := (PasswordHasher{}).Compare(tt.hash, tt.password)
			if matched != tt.wantMatch || !errors.Is(err, tt.wantErr) {
				t.Fatalf("Compare() = (%v, %v), want (%v, %v)", matched, err, tt.wantMatch, tt.wantErr)
			}
		})
	}
}
