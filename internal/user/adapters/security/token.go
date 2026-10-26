package security

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken: токен испорчен, истёк или подписан другим ключом.
var ErrInvalidToken = errors.New("invalid access token")

type AccessToken struct {
	secret []byte
	ttl    time.Duration
}

func NewAccessToken(secret []byte, ttl time.Duration) *AccessToken {
	return &AccessToken{secret: secret, ttl: ttl}
}

func (t *AccessToken) Issue(userID int64) (string, error) {
	token, _, err := t.IssueAccess(userID)
	return token, err
}

// IssueAccess returns the exact expiration recorded in the signed JWT.
func (t *AccessToken) IssueAccess(userID int64) (string, time.Time, error) {
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(t.ttl)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, claims.ExpiresAt.Time, nil
}

func (t *AccessToken) Parse(token string) (int64, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: bad subject: %w", ErrInvalidToken, err)
	}
	return id, nil
}
