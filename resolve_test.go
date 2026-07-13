package sconf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvislobokov/sconf/secret"
)

// resolveTestConfig — конфигурация с полем-секретом для сквозных тестов Load.
type resolveTestConfig struct {
	Host   string       `default:"localhost"`
	APIKey secret.Value `yaml:"api_key"`
}

// plainConfig — конфигурация без секретов.
type plainConfig struct {
	Host string `default:"localhost"`
}

// clearVaultEnv отвязывает тест от окружения Vault на машине разработчика/CI.
func clearVaultEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"VAULT_ADDR", "VAULT_URL", "VAULT_TOKEN", "VAULT_SECRETS_FILE"} {
		t.Setenv(k, "")
	}
}

// writeSecretsFile пишет локальный файл секретов (формат VAULT_SECRETS_FILE).
func writeSecretsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadResolvesSecrets(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv("VAULT_SECRETS_FILE", writeSecretsFile(t, "secret/data/myapp:\n  api_key: ak_test_123\n"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // останавливает фоновое обновление секретов

	cfg, err := LoadContext[resolveTestConfig](ctx,
		New().AddInMemory(map[string]string{"api_key": "secret/data/myapp?field=api_key"}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.APIKey.Get(); got != "ak_test_123" {
		t.Fatalf("api key = %q, want %q", got, "ak_test_123")
	}
	if cfg.Host != "localhost" {
		t.Fatalf("host default lost: %q", cfg.Host)
	}
}

func TestLoadSecretsWatchStopsOnCancel(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv("VAULT_SECRETS_FILE", writeSecretsFile(t, "secret/data/myapp:\n  api_key: ak_test_123\n"))

	ctx, cancel := context.WithCancel(context.Background())
	cfg, err := LoadContext[resolveTestConfig](ctx,
		New().AddInMemory(map[string]string{"api_key": "secret/data/myapp?field=api_key&refresh=1h"}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey.Refresh() <= 0 {
		t.Fatal("secret has no refresh interval, watcher would not track it")
	}
	cancel() // горутины обновления привязаны к ctx и завершаются
}

func TestLoadSecretsNotConfigured(t *testing.T) {
	clearVaultEnv(t)

	_, err := Load[resolveTestConfig](
		New().AddInMemory(map[string]string{"api_key": "secret/data/myapp?field=api_key"}),
		nil,
	)
	if !errors.Is(err, ErrVaultNotConfigured) {
		t.Fatalf("expected ErrVaultNotConfigured, got %v", err)
	}
}

func TestLoadNoSecretsNoVaultNeeded(t *testing.T) {
	clearVaultEnv(t)

	cfg, err := Load[plainConfig](New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "localhost" {
		t.Fatalf("host = %q", cfg.Host)
	}
}
