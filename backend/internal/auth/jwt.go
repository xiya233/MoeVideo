package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Manager struct {
	secret []byte
}

type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

func (m *Manager) GenerateAccessToken(userID, username string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

func (m *Manager) ParseAccessToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
