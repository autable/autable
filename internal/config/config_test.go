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
server:
  public_url: https://app.example
repository:
  path: ./repo
  remote_url: https://example.com/autable/repository.git
  remote_branch: main
auth:
  admins:
    - Admin@Example.com
  password:
    enabled: true
  oidc:
    enabled: true
    providers:
      - name: main
        issuer_url: https://issuer.example
        client_id: autable
debug:
  pprof_address: 127.0.0.1:6060
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
	if got := cfg.Server.PublicURL; got != "https://app.example" {
		t.Fatalf("unexpected server public url: %q", got)
	}
	if cfg.Debug.PprofAddress != "127.0.0.1:6060" {
		t.Fatalf("unexpected debug config: %#v", cfg.Debug)
	}
	if len(cfg.Auth.Admins) != 1 || cfg.Auth.Admins[0] != "Admin@Example.com" {
		t.Fatalf("unexpected admins: %#v", cfg.Auth.Admins)
	}
}

func TestAuthConfigIsAdminMatchesEmailCaseInsensitively(t *testing.T) {
	auth := AuthConfig{Admins: []string{" Admin@Example.com "}}
	for _, email := range []string{"admin@example.com", "ADMIN@EXAMPLE.COM", " admin@example.com"} {
		if !auth.IsAdmin(email) {
			t.Fatalf("expected %q to be an admin", email)
		}
	}
	for _, email := range []string{"", "other@example.com", "workflow:1", "admin@example.co"} {
		if auth.IsAdmin(email) {
			t.Fatalf("expected %q not to be an admin", email)
		}
	}
	if (AuthConfig{}).IsAdmin("admin@example.com") {
		t.Fatal("expected no admins without configuration")
	}
}

func TestValidateRejectsAdminsThatAreNotEmails(t *testing.T) {
	cfg := Config{
		Data:       DataConfig{Path: "./data"},
		Repository: RepositoryConfig{Path: "./repo", RemoteURL: "https://example.com/repo.git", RemoteBranch: "main"},
		Auth: AuthConfig{
			Admins:   []string{"admin@example.com", " "},
			Password: PasswordAuthConfig{Enabled: true},
		},
	}
	if err := cfg.Validate(); err == nil || err.Error() != `auth.admins[1] must be an email address, got " "` {
		t.Fatalf("expected admin validation error, got %v", err)
	}
	cfg.Auth.Admins = []string{"admin@example.com"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRequiresCorePaths(t *testing.T) {
	err := (Config{}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRepositoryRemoteOnlyWhenEnabled(t *testing.T) {
	disabled := false
	cfg := Config{
		Data:       DataConfig{Path: "./data"},
		Repository: RepositoryConfig{Enabled: &disabled, Path: "./repo"},
		Auth:       AuthConfig{Password: PasswordAuthConfig{Enabled: true}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected disabled repository to skip remote validation, got %v", err)
	}

	cfg.Repository.Enabled = nil
	err := cfg.Validate()
	if err == nil || err.Error() != "repository.remote_url is required when repository.enabled is true" {
		t.Fatalf("expected repository to default to enabled, got %v", err)
	}

	enabled := true
	cfg.Repository.Enabled = &enabled
	cfg.Repository.RemoteURL = "https://example.com/repo.git"
	err = cfg.Validate()
	if err == nil || err.Error() != "repository.remote_branch is required when repository.enabled is true" {
		t.Fatalf("expected enabled repository to require remote branch, got %v", err)
	}

	cfg.Repository.Enabled = &disabled
	cfg.Repository.Path = ""
	err = cfg.Validate()
	if err == nil || err.Error() != "repository.path is required" {
		t.Fatalf("expected repository path to stay required when disabled, got %v", err)
	}
}

func TestLoadDisabledRepositoryConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	data := []byte(`
data:
  path: ./data
repository:
  enabled: false
  path: ./repo
auth:
  password:
    enabled: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repository.IsEnabled() {
		t.Fatal("expected repository to be disabled")
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

	cfg.Auth.OIDC.Providers = []OIDCProvider{{
		Name:      "main",
		IssuerURL: "https://issuer.example",
		ClientID:  "autable",
	}}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected server.public_url validation error")
	}

	cfg.Server.PublicURL = "https://app.example"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRequiresBackupBucketWhenEnabled(t *testing.T) {
	cfg := Config{
		Data:       DataConfig{Path: "./data"},
		Repository: RepositoryConfig{Path: "./repo", RemoteURL: "https://example.com/repo.git", RemoteBranch: "main"},
		Backup:     BackupConfig{Enabled: true},
		Auth: AuthConfig{
			Password: PasswordAuthConfig{Enabled: true},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
	cfg.S3.Connection.Bucket = "autable-backups"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestLoadS3ConfigWithDirectoryDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	data := []byte(`
data:
  path: ./data
repository:
  enabled: false
  path: ./repo
auth:
  password:
    enabled: true
s3:
  connection:
    endpoint: https://s3.example.com
    bucket: autable
    access_key_id: key
    secret_access_key: secret
    force_path_style: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.S3.IsConfigured() || cfg.S3.Connection.Bucket != "autable" || !cfg.S3.Connection.ForcePathStyle {
		t.Fatalf("unexpected s3 connection: %#v", cfg.S3.Connection)
	}
	if cfg.S3.Directories.Backup != "backup" || cfg.S3.Directories.Files != "files" {
		t.Fatalf("expected default directories, got %#v", cfg.S3.Directories)
	}
	if cfg.Files.MaxUploadSizeMB != 20 {
		t.Fatalf("expected default upload limit of 20 MB, got %d", cfg.Files.MaxUploadSizeMB)
	}
}
