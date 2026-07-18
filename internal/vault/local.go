package vault

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dvislobokov/sconf/secret"
	"gopkg.in/yaml.v3"
)

// fileStore — источник секретов для локальной разработки: значения берутся из
// файла (YAML или JSON), а не из Vault. Включается переменной VAULT_SECRETS_FILE
// либо наличием файла vault.secrets в рабочей директории; при этом VAULT_ADDR и
// аутентификация не требуются.
//
// Формат файла — отображение «полный путь секрета → его поля» (те же пути, что
// и в конфиге приклада). Поля кладутся так, как их отдал бы Vault:
//
//	database/static-creds/billing-app:
//	  username: devuser
//	  password: devpass
//
//	pki/issue/web:
//	  certificate: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"
//	  private_key: "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
//	  serial_number: dev
//
//	secret/data/billing:
//	  stripe_key: sk_test_local
//	  region: eu-central-1
//
// Файл с секретами разработчика не коммитится (добавьте в .gitignore).
type fileStore struct {
	path    string
	secrets map[string]map[string]any
}

// newFileStore читает и разбирает файл секретов.
func newFileStore(path string) (store, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read local secrets file %q: %w", path, err)
	}
	var m map[string]map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("vault: parse local secrets file %q: %w", path, err)
	}
	return fileStore{path: path, secrets: m}, nil
}

// fetch возвращает поля секрета по пути. VAULT_MOUNTPATH здесь не применяется —
// в файле указываются те же пути, что и в конфиге. Аренды (lease) нет, поэтому
// интервал обновления берётся по умолчанию (или из ?refresh=).
func (f fileStore) fetch(_ context.Context, req secret.Request) (map[string]any, time.Duration, error) {
	data, ok := f.secrets[req.Path]
	if !ok {
		return nil, 0, fmt.Errorf("vault: secret %q not found in local secrets file %q", req.Path, f.path)
	}
	return data, 0, nil
}
