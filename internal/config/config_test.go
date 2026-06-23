package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	data := []byte(`
data:
  path: ./data
repository:
  path: ./repo
  remote_url: https://example.com/autable/repository.git
  remote_branch: main
auth:
  password:
    enabled: true
  oidc:
    enabled: true
    providers:
      - name: main
        issuer_url: https://issuer.example
        client_id: autable
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Data.Path != "./data" {
		t.Fatalf("unexpected data path: %q", cfg.Data.Path)
	}
	if cfg.SystemDBPath() != filepath.Join("./data", "system.sqlite") {
		t.Fatalf("unexpected system db path: %q", cfg.SystemDBPath())
	}
	if cfg.HistoryPath() != filepath.Join("./data", "leveldb") {
		t.Fatalf("unexpected history path: %q", cfg.HistoryPath())
	}
	if got := cfg.Auth.OIDC.Providers[0].Name; got != "main" {
		t.Fatalf("unexpected provider name: %q", got)
	}
}

func TestValidateRequiresCorePaths(t *testing.T) {
	err := (Config{}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRequiresAtLeastOneAuthMethod(t *testing.T) {
	cfg := Config{
		Data:       DataConfig{Path: "./data"},
		Repository: RepositoryConfig{Path: "./repo", RemoteURL: "https://example.com/repo.git", RemoteBranch: "main"},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateOIDCProvidersOnlyWhenEnabled(t *testing.T) {
	cfg := Config{
		Data:       DataConfig{Path: "./data"},
		Repository: RepositoryConfig{Path: "./repo", RemoteURL: "https://example.com/repo.git", RemoteBranch: "main"},
		Auth: AuthConfig{
			Password: PasswordAuthConfig{Enabled: true},
			OIDC: OIDCConfig{
				Enabled: false,
				Providers: []OIDCProvider{
					{Name: "incomplete"},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	cfg.Auth.Password.Enabled = false
	cfg.Auth.OIDC.Enabled = true
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}
