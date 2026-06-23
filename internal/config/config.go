package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Data       DataConfig       `yaml:"data"`
	Repository RepositoryConfig `yaml:"repository"`
	Auth       AuthConfig       `yaml:"auth"`
}

type ServerConfig struct {
	Address string `yaml:"address"`
}

type DataConfig struct {
	Path string `yaml:"path"`
}

type RepositoryConfig struct {
	Path         string               `yaml:"path"`
	RemoteURL    string               `yaml:"remote_url"`
	RemoteBranch string               `yaml:"remote_branch"`
	Sync         RepositorySyncConfig `yaml:"sync"`
}

type RepositorySyncConfig struct {
	Debounce    string `yaml:"debounce"`
	PushTimeout string `yaml:"push_timeout"`
	AuthorName  string `yaml:"author_name"`
	AuthorEmail string `yaml:"author_email"`
}

type AuthConfig struct {
	Password PasswordAuthConfig `yaml:"password"`
	OIDC     OIDCConfig         `yaml:"oidc"`
}

type PasswordAuthConfig struct {
	Enabled bool `yaml:"enabled"`
}

type OIDCConfig struct {
	Enabled   bool           `yaml:"enabled"`
	Providers []OIDCProvider `yaml:"providers"`
}

type OIDCProvider struct {
	Name         string   `yaml:"name"`
	IssuerURL    string   `yaml:"issuer_url"`
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	Scopes       []string `yaml:"scopes"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	if cfg.Data.Path == "" {
		return errors.New("data.path is required")
	}
	if cfg.Repository.Path == "" {
		return errors.New("repository.path is required")
	}
	if cfg.Repository.RemoteURL == "" {
		return errors.New("repository.remote_url is required")
	}
	if cfg.Repository.RemoteBranch == "" {
		return errors.New("repository.remote_branch is required")
	}
	if !cfg.Auth.Password.Enabled && !cfg.Auth.OIDC.Enabled {
		return errors.New("at least one auth method is required")
	}
	if !cfg.Auth.OIDC.Enabled {
		return nil
	}
	if len(cfg.Auth.OIDC.Providers) == 0 {
		return errors.New("auth.oidc.providers is required when auth.oidc.enabled is true")
	}
	for i, provider := range cfg.Auth.OIDC.Providers {
		if provider.Name == "" {
			return fmt.Errorf("auth.oidc.providers[%d].name is required", i)
		}
		if provider.IssuerURL == "" {
			return fmt.Errorf("auth.oidc.providers[%d].issuer_url is required", i)
		}
		if provider.ClientID == "" {
			return fmt.Errorf("auth.oidc.providers[%d].client_id is required", i)
		}
	}
	return nil
}

func (cfg Config) SystemDBPath() string {
	return filepath.Join(cfg.Data.Path, "system.sqlite")
}

func (cfg Config) HistoryPath() string {
	return filepath.Join(cfg.Data.Path, "leveldb")
}

func (cfg Config) DatabasePath(name string) string {
	return filepath.Join(cfg.Data.Path, name+".sqlite")
}
