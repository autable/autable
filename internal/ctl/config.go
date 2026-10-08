package ctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// credentials are what `login` stores: one server and the session token
// issued for it.
type credentials struct {
	Server    string `json:"server"`
	Token     string `json:"token"`
	Email     string `json:"email,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

func defaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autable", "autablectl.json"), nil
}

func loadCredentials(path string) (credentials, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentials{}, false, nil
	}
	if err != nil {
		return credentials{}, false, err
	}
	var creds credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return credentials{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return creds, true, nil
}

func saveCredentials(path string, creds credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func removeCredentials(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
