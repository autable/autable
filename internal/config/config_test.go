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
system_db:
  path: ./system.sqlite
history:
  path: ./history
repository:
  path: ./repo
oidc:
  providers:
    - name: main
      issuer_url: https://issuer.example
      client_id: codetable
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemDB.Path != "./system.sqlite" {
		t.Fatalf("unexpected system db path: %q", cfg.SystemDB.Path)
	}
	if got := cfg.OIDC.Providers[0].Name; got != "main" {
		t.Fatalf("unexpected provider name: %q", got)
	}
}

func TestValidateRequiresCorePaths(t *testing.T) {
	err := (Config{}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}
