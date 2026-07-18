package sconf

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	yaml "gopkg.in/yaml.v3"

	"github.com/dvislobokov/sconf/bind"
	"github.com/dvislobokov/sconf/internal/flat"
	"github.com/dvislobokov/sconf/provider"
)

// helpFormat извлекает значение --format из аргументов ("--format env" или
// "--format=env"). Пустая строка — формат не задан (таблица по умолчанию).
func helpFormat(args []string) string {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--format="); ok {
			return v
		}
		if a == "--format" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// usageDoc — одна запись схемы конфигурации для машиночитаемых форматов.
type usageDoc struct {
	Key         string   `json:"key" yaml:"key" toml:"key"`
	Env         string   `json:"env" yaml:"env" toml:"env"`
	Type        string   `json:"type" yaml:"type" toml:"type"`
	Default     string   `json:"default,omitempty" yaml:"default,omitempty" toml:"default,omitempty"`
	Enum        []string `json:"enum,omitempty" yaml:"enum,omitempty" toml:"enum,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty" toml:"description,omitempty"`
}

// UsageFormat генерирует справку по типу T в указанном формате:
//
//	table              — человекочитаемая таблица (как Usage);
//	env                — шаблон .env: имя переменной и значение по умолчанию,
//	                     описание и тип комментарием;
//	json | yaml | toml — схема ключей списком (key, env, type, default,
//	                     enum, description).
//
// envPrefix — префикс имён переменных среды (как у AddEnvironmentVariables);
// поле с тегом env использует имя из тега как есть, без префикса.
//
// Load отвечает на "--help --format <f>" именно этим выводом, определяя
// envPrefix по env-провайдеру билдера.
func UsageFormat[T any](format, envPrefix string) (string, error) {
	entries := bind.Describe(typeOf[T]())

	switch format {
	case "", "table":
		return Usage[T](), nil

	case "env":
		var b strings.Builder
		for _, e := range entries {
			fmt.Fprintf(&b, "# %s", strings.TrimSpace(e.Description+" ("+e.Type))
			if len(e.Enum) > 0 {
				fmt.Fprintf(&b, ", one of %s", strings.Join(e.Enum, "|"))
			}
			if e.HasDefault {
				fmt.Fprintf(&b, ", default %q", e.Default)
			}
			b.WriteString(")\n")
			fmt.Fprintf(&b, "%s=%s\n", entryEnvName(e, envPrefix), envValue(e.Default))
		}
		return b.String(), nil

	case "json":
		out, err := json.MarshalIndent(usageDocs(entries, envPrefix), "", "  ")
		if err != nil {
			return "", fmt.Errorf("config: usage json: %w", err)
		}
		return string(out) + "\n", nil

	case "yaml":
		out, err := yaml.Marshal(usageDocs(entries, envPrefix))
		if err != nil {
			return "", fmt.Errorf("config: usage yaml: %w", err)
		}
		return string(out), nil

	case "toml":
		out, err := toml.Marshal(struct {
			Options []usageDoc `toml:"options"`
		}{usageDocs(entries, envPrefix)})
		if err != nil {
			return "", fmt.Errorf("config: usage toml: %w", err)
		}
		return string(out), nil

	default:
		return "", fmt.Errorf("config: unknown help format %q (want table|env|json|yaml|toml)", format)
	}
}

// builtinUsage — встроенные флаги Load, не являющиеся ключами конфигурации.
// Печатаются в конце табличного ответа на --help; в машиночитаемые форматы
// (env/json/yaml/toml — схема ключей) и в HTTP-хендлер usage не попадают.
const builtinUsage = `
Built-in flags:
  --help, -h, -?                     print this help and exit
  --format table|env|json|yaml|toml  help output format (use with --help)
`

// helpOutput возвращает текст ответа на --help: usage в запрошенном формате;
// к табличному виду добавляются встроенные флаги самой Load.
func helpOutput[T any](format, envPrefix string) (string, error) {
	out, err := UsageFormat[T](format, envPrefix)
	if err != nil {
		return "", err
	}
	if format == "" || format == "table" {
		out += builtinUsage
	}
	return out, nil
}

func usageDocs(entries []bind.Entry, envPrefix string) []usageDoc {
	out := make([]usageDoc, 0, len(entries))
	for _, e := range entries {
		out = append(out, usageDoc{
			Key:         e.Key,
			Env:         entryEnvName(e, envPrefix),
			Type:        e.Type,
			Default:     e.Default,
			Enum:        e.Enum,
			Description: e.Description,
		})
	}
	return out
}

// entryEnvName возвращает имя переменной среды для записи: тег env как есть,
// иначе prefix + путь в env-нотации (SERVERS__N__HOST для элементов срезов).
func entryEnvName(e bind.Entry, envPrefix string) string {
	if e.EnvVar != "" {
		return e.EnvVar
	}
	return envPrefix + envName(flat.Normalize(e.Key))
}

// builderEnvPrefix возвращает префикс первого env-провайдера билдера
// (для показа полных имён переменных в справке).
func builderEnvPrefix(b *Builder) string {
	for _, p := range b.providers {
		if e, ok := p.(*provider.EnvProvider); ok {
			return e.Prefix()
		}
	}
	return ""
}

// envOverrides строит индекс "нормализованный ключ -> имя переменной из тега
// env" для типа t (используется дампом в формате DumpEnv).
func envOverrides(t reflect.Type) map[string]string {
	out := map[string]string{}
	for _, e := range bind.Describe(t) {
		if e.EnvVar != "" && !hasPlaceholder(e.Key) {
			out[flat.Normalize(e.Key)] = e.EnvVar
		}
	}
	return out
}

// envTagValues читает значения переменных среды, названных тегом env у полей
// T, и возвращает их как пары "путь конфигурации -> значение". Поля внутри
// элементов срезов и map (плейсхолдеры N и <key> в пути) пропускаются: одна
// переменная не может адресовать конкретный элемент.
func envTagValues[T any]() map[string]string {
	out := map[string]string{}
	for _, e := range bind.Describe(typeOf[T]()) {
		if e.EnvVar == "" || hasPlaceholder(e.Key) {
			continue
		}
		if v, ok := os.LookupEnv(e.EnvVar); ok {
			out[e.Key] = v
		}
	}
	return out
}

func hasPlaceholder(key string) bool {
	for _, seg := range strings.Split(key, ":") {
		if seg == "N" || seg == "<key>" {
			return true
		}
	}
	return false
}
