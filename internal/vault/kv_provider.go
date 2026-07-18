package vault

import (
	"context"

	"github.com/dvislobokov/sconf/internal/flat"
	"github.com/dvislobokov/sconf/secret"
)

// kvProvider — источник конфигурации sconf, читающий KV-секрет Vault и
// раскладывающий его поля прямо в дерево конфигурации. В отличие от полей-
// секретов (secret.KV/UserPass/...), которые заполняют одно поле структуры,
// провайдер добавляет ключи секрета как обычный слой конфигурации — они
// биндятся в любые поля наравне со значениями из файлов и переменных среды.
//
// Вложенные объекты и списки раскладываются так же, как в остальных источниках
// (ключ:подключ, список:индекс), поэтому в KV можно хранить хоть целое поддерево
// настроек.
type kvProvider struct {
	path   string
	prefix string
}

// KVOption настраивает kvProvider.
type KVOption func(*kvProvider)

// At помещает поля секрета под указанный префикс (секцию) конфигурации.
// Без него ключи секрета попадают в корень конфигурации.
func At(prefix string) KVOption {
	return func(p *kvProvider) { p.prefix = prefix }
}

// KV возвращает провайдер конфигурации, читающий KV-секрет Vault по полному
// пути (для KV v2 — с сегментом data, например "secret/data/myapp") и
// добавляющий его поля в конфигурацию. Подключение к Vault берётся из тех же
// переменных среды, что и резолвер (VAULT_ADDR, VAULT_AUTH, ...).
// Наружу выставляется методами Builder.AddVaultKV / Builder.AddVaultKVAt.
func KV(path string, opts ...KVOption) *kvProvider {
	p := &kvProvider{path: path}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Load читает секрет и возвращает его поля как плоские пары путь→значение.
// Реализует интерфейс sconf.Provider. Как и поля-секреты, в режиме локальной
// разработки (VAULT_SECRETS_FILE либо файл vault.secrets в рабочей директории)
// читает из файла, а не из Vault. Ожидание
// доступности Vault включается переменными среды VAULT_WAIT /
// VAULT_WAIT_INTERVAL (см. waitReady).
func (p *kvProvider) Load() (map[string]string, error) {
	ctx := context.Background()
	wait, err := loadWait(waitOptions{})
	if err != nil {
		return nil, err
	}

	var data map[string]any
	err = waitReady(ctx, wait, func() error {
		src, err := newStore(ctx)
		if err != nil {
			return err
		}
		data, _, err = src.fetch(ctx, secret.Request{Method: secret.Read, Path: p.path})
		return err
	})
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	flat.Flatten(out, p.prefix, secret.KVFields(data))
	return out, nil
}
