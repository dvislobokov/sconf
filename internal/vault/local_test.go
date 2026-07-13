package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvislobokov/sconf/secret"
)

func writeSecretsFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "secrets.dev.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLocalFileResolve(t *testing.T) {
	file := writeSecretsFile(t, `
database/static-creds/app:
  username: devuser
  password: devpass
pki/issue/web:
  certificate: DEVCERT
  private_key: DEVKEY
  serial_number: dev-01
secret/data/billing:
  stripe_key: sk_test_local
  region: eu-central-1
`)
	// Только VAULT_SECRETS_FILE — VAULT_ADDR не задан (в Vault не ходим).
	withEnv(t, map[string]string{"VAULT_SECRETS_FILE": file})

	cfg := struct {
		DB   secret.UserPass
		Cert secret.Cert
		Key  secret.Value
	}{}
	_ = cfg.DB.UnmarshalConfig("database/static-creds/app")
	_ = cfg.Cert.UnmarshalConfig("pki/issue/web?common_name=x")
	_ = cfg.Key.UnmarshalConfig("secret/data/billing?field=stripe_key")

	if err := Resolve(context.Background(), &cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.DB.Username() != "devuser" || cfg.DB.Password() != "devpass" {
		t.Fatalf("db = %q/%q", cfg.DB.Username(), cfg.DB.Password())
	}
	if cfg.Cert.Certificate() != "DEVCERT" || cfg.Cert.SerialNumber() != "dev-01" {
		t.Fatalf("cert = %q/%q", cfg.Cert.Certificate(), cfg.Cert.SerialNumber())
	}
	if cfg.Key.Get() != "sk_test_local" {
		t.Fatalf("key = %q", cfg.Key.Get())
	}
}

func TestLocalFileMissingPath(t *testing.T) {
	file := writeSecretsFile(t, "some/path:\n  a: b\n")
	withEnv(t, map[string]string{"VAULT_SECRETS_FILE": file})

	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("missing/path")
	if err := Resolve(context.Background(), &cfg); err == nil {
		t.Fatal("expected error for path missing from local secrets file")
	}
}

func TestLocalFileProvider(t *testing.T) {
	file := writeSecretsFile(t, `
secret/data/app:
  log_level: debug
  nested:
    host: localhost
`)
	withEnv(t, map[string]string{"VAULT_SECRETS_FILE": file})

	out, err := KV("secret/data/app", At("cfg")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if out["cfg:log_level"] != "debug" || out["cfg:nested:host"] != "localhost" {
		t.Fatalf("out = %v", out)
	}
}

func TestLocalFileTakesPrecedenceOverVault(t *testing.T) {
	file := writeSecretsFile(t, "db/creds/app:\n  username: local\n  password: p\n")
	// Даже если задан VAULT_ADDR, файл имеет приоритет — реального Vault нет,
	// и если бы пошли в него, тест упал бы на подключении.
	withEnv(t, map[string]string{
		"VAULT_SECRETS_FILE": file,
		"VAULT_ADDR":         "http://127.0.0.1:1", // недоступен
		"VAULT_TOKEN":        "x",
	})
	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("db/creds/app")
	if err := Resolve(context.Background(), &cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.DB.Username() != "local" {
		t.Fatalf("username = %q", cfg.DB.Username())
	}
}
