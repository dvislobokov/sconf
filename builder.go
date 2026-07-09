package sconf

import (
	"github.com/dvislobokov/sconf/internal/flat"
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

// AddCommandLine добавляет аргументы командной строки (обычно os.Args[1:]).
func (b *Builder) AddCommandLine(args []string) *Builder {
	return b.Add(provider.Args(args))
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
