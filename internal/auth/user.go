package auth

import (
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Provider string

const (
	ProviderPassword Provider = "password"
	ProviderOIDC     Provider = "oidc"
)

type User struct {
	ID           string
	Email        string
	Provider     Provider
	ProviderName string
	Subject      string
	PasswordHash []byte
}

type PasswordRegistration struct {
	Email    string
	Password string
}

type OIDCIdentity struct {
	ProviderName string
	Subject      string
	Email        string
}

func NewPasswordUser(reg PasswordRegistration) (User, error) {
	email, err := NormalizeEmail(reg.Email)
	if err != nil {
		return User{}, err
	}
	if len(reg.Password) < 8 {
		return User{}, errors.New("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(reg.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	return User{
		ID:           uuid.NewString(),
		Email:        email,
		Provider:     ProviderPassword,
		ProviderName: string(ProviderPassword),
		Subject:      email,
		PasswordHash: hash,
	}, nil
}

func NewOIDCUser(identity OIDCIdentity) (User, error) {
	email, err := NormalizeEmail(identity.Email)
	if err != nil {
		return User{}, err
	}
	if identity.ProviderName == "" {
		return User{}, errors.New("provider name is required")
	}
	if identity.Subject == "" {
		return User{}, errors.New("subject is required")
	}

	return User{
		ID:           uuid.NewString(),
		Email:        email,
		Provider:     ProviderOIDC,
		ProviderName: identity.ProviderName,
		Subject:      identity.Subject,
	}, nil
}

func NormalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if _, err := mail.ParseAddress(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

func (user User) CheckPassword(password string) bool {
	if user.Provider != ProviderPassword || len(user.PasswordHash) == 0 {
		return false
	}
	return bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)) == nil
}

func SameLoginFallback(left, right User) bool {
	return left.Email != "" && left.Email == right.Email
}
