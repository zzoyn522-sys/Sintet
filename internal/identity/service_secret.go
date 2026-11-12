package identity

import (
	"errors"
	"os"
	"sintet/internal/accountfiles"
)

// Service keys use the same Windows account-bound protection as saved login credentials.
func SaveServiceSecret(path, value string) error {
	if value == "" {
		return errors.New("empty service credential")
	}
	data, err := protect([]byte(value))
	if err != nil {
		return err
	}
	return accountfiles.Write(path, data)
}
func ReadServiceSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	plain, err := unprotect(data)
	return string(plain), err
}
