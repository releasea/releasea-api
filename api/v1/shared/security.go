package shared

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func VerifyPassword(password, stored string) bool {
	if password == "" || stored == "" {
		return false
	}
	if IsBcryptHash(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) == nil
	}
	return stored == password
}

func IsBcryptHash(value string) bool {
	return strings.HasPrefix(value, "$2a$") || strings.HasPrefix(value, "$2b$") || strings.HasPrefix(value, "$2y$")
}
