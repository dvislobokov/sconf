package sconf

import (
	"github.com/dvislobokov/sconf/internal/flat"
	"github.com/dvislobokov/sconf/internal/vault"
	"github.com/dvislobokov/sconf/provider"
)

// Provider — источник конфигурации. Возвращает плоские пары "путь -> значение".
type Provider interface {
	Load() (map[string]string, error)
}

// Builder собирает конфигурацию из упорядоченного набора провайдеров.
// Провайдеры, добавленные позже, перекрывают значения предыдущих.
type Builder struct {
	providers []Provider
}

// New создаёт пустой Builder.
func New() *Builder { return &Builder{} }

// Add добавляет произвольный источник конфигурации.
func (b *Builder) Add(p Provider) *Builder {
	b.providers = append(b.providers, p)
	return b
}

// AddJSONFile добавляет JSON-файл.
func (b *Builder) AddJSONFile(path string, opts ...FileOption) *Builder {
	return b.Add(provider.JSONFile(path, opts...))
}

// AddYAMLFile добавляет YAML-файл.
func (b *Builder) AddYAMLFile(path string, opts ...FileOption) *Builder {
	return b.Add(provider.YAMLFile(path, opts...))
}

// AddTOMLFile добавляет TOML-файл.
func (b *Builder) AddTOMLFile(path string, opts ...FileOption) *Builder {
	return b.Add(provider.TOMLFile(path, opts...))
}

// AddEnvironmentVariables добавляет переменные среды. prefix (может быть пустым)
// отсекается от имени переменной; "__" превращается в ":".
func (b *Builder) AddEnvironmentVariables(prefix string) *Builder {
	return b.Add(provider.Env(prefix))
}

// AddDotEnvFile добавляет .env-файл: строки KEY=VALUE трактуются как
// переменные среды (prefix отсекается, "__" превращается в ":"). Удобно для
// локальной разработки — тот же файл, что скармливается direnv или docker
// compose, читается напрямую, без экспорта в окружение. Реальные переменные
// среды процесса не затрагиваются.
func (b *Builder) AddDotEnvFile(path, prefix string, opts ...FileOption) *Builder {
	return b.Add(provider.DotEnvFile(path, prefix, opts...))
}

// AddCommandLine добавляет аргументы командной строки (обычно os.Args[1:]).
func (b *Builder) AddCommandLine(args []string) *Builder {
	return b.Add(provider.Args(args))
}

// AddVaultKV добавляет KV-секрет Vault как источник конфигурации: его поля
// раскладываются в корень дерева наравне со значениями из файлов и переменных
// среды. path — полный путь секрета (для KV v2 — с сегментом data, например
// "secret/data/myapp"). Подключение к Vault берётся из тех же переменных
// среды, что и у полей-секретов (VAULT_ADDR, VAULT_AUTH, ...); в режиме
// локальной разработки (VAULT_SECRETS_FILE либо файл vault.secrets в рабочей
// директории) секрет читается из файла.
func (b *Builder) AddVaultKV(path string) *Builder {
	return b.Add(vault.KV(path))
}

// AddVaultKVAt — как AddVaultKV, но помещает поля секрета под указанный
// префикс (секцию) конфигурации, например AddVaultKVAt("secret/data/db",
// "database").
func (b *Builder) AddVaultKVAt(path, section string) *Builder {
	return b.Add(vault.KV(path, vault.At(section)))
}

// AddInMemory добавляет заранее заданные значения.
func (b *Builder) AddInMemory(values map[string]string) *Builder {
	return b.Add(provider.Map(values))
}

// Build загружает все провайдеры по порядку и мержит их значения per key.
func (b *Builder) Build() (*Config, error) {
	m := flat.New()
	for _, p := range b.providers {
		kv, err := p.Load()
		if err != nil {
			return nil, err
		}
		m.SetAll(kv)
	}
	return &Config{m: m}, nil
}
