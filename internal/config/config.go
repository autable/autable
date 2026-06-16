package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	SystemDB   SystemDBConfig   `yaml:"system_db"`
	History    HistoryConfig    `yaml:"history"`
	Repository RepositoryConfig `yaml:"repository"`
	OIDC       OIDCConfig       `yaml:"oidc"`
}

type ServerConfig struct {
	Address string `yaml:"address"`
}

type SystemDBConfig struct {
	Path string `yaml:"path"`
}

type HistoryConfig struct {
	Path string `yaml:"path"`
}

type RepositoryConfig struct {
	Path string `yaml:"path"`
}

type OIDCConfig struct {
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
	if cfg.SystemDB.Path == "" {
		return errors.New("system_db.path is required")
	}
	if cfg.History.Path == "" {
		return errors.New("history.path is required")
	}
	if cfg.Repository.Path == "" {
		return errors.New("repository.path is required")
	}
	for i, provider := range cfg.OIDC.Providers {
		if provider.Name == "" {
			return fmt.Errorf("oidc.providers[%d].name is required", i)
		}
		if provider.IssuerURL == "" {
			return fmt.Errorf("oidc.providers[%d].issuer_url is required", i)
		}
		if provider.ClientID == "" {
			return fmt.Errorf("oidc.providers[%d].client_id is required", i)
		}
	}
	return nil
}
