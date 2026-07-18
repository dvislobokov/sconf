package sconf

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	yaml "gopkg.in/yaml.v3"

	"github.com/dvislobokov/sconf/bind"
	"github.com/dvislobokov/sconf/internal/flat"
)

// DumpFormat — формат вывода Dump.
type DumpFormat string

const (
	// DumpKeys — плоские пары "key = value" (ключи через ":").
	DumpKeys DumpFormat = "keys"
	// DumpEnv — переменные среды: KEY__SUB=value, совместимо с .env-файлами
	// и AddEnvironmentVariables.
	DumpEnv DumpFormat = "env"
	// DumpJSON — вложенный JSON.
	DumpJSON DumpFormat = "json"
	// DumpYAML — вложенный YAML.
	DumpYAML DumpFormat = "yaml"
	// DumpTOML — вложенный TOML.
	DumpTOML DumpFormat = "toml"
)

// dumpOptions — настройки Dump.
type dumpOptions struct {
	envPrefix string
	redact    []string
}

// DumpOption настраивает Dump.
type DumpOption func(*dumpOptions)

// WithDumpEnvPrefix задаёт префикс имён переменных для формата DumpEnv
// (например "APP_").
func WithDumpEnvPrefix(prefix string) DumpOption {
	return func(o *dumpOptions) { o.envPrefix = prefix }
}

// WithDumpRedact маскирует значения перечисленных ключей (и всех ключей под
// ними) как "***". Полезно, когда в конфигурации лежат чувствительные
// значения — например, слой AddVaultKV.
func WithDumpRedact(keys ...string) DumpOption {
	return func(o *dumpOptions) {
		for _, k := range keys {
			o.redact = append(o.redact, flat.Normalize(k))
		}
	}
}

// Dump печатает итоговую (слитую из всех слоёв) конфигурацию cfg в указанном
// формате. T — тип конфигурации приклада: из его тегов description/usage
// берутся комментарии к ключам. Если описания не нужны, используйте
// DumpValues.
//
// Описания выводятся комментариями "#" в форматах DumpKeys и DumpEnv.
// DumpJSON/DumpYAML/DumpTOML печатают чистые данные: JSON комментариев не
// поддерживает, а сериализаторы YAML/TOML их не пишут.
//
// Все значения выводятся строками — такова внутренняя модель конфигурации.
// Для секции (Config.Section) печатаются только её ключи, без префикса.
//
// Значения секретных полей (secret.UserPass и т.п.) в конфигурации — это
// пути в Vault, не сами секреты. Но слой AddVaultKV и plain:-значения
// секретов кладут в конфигурацию реальные секреты — маскируйте их через
// WithDumpRedact.
func Dump[T any](cfg *Config, format DumpFormat, opts ...DumpOption) (string, error) {
	var o dumpOptions
	for _, op := range opts {
		op(&o)
	}
	kv := dumpPairs(cfg, o)

	switch format {
	case DumpKeys:
		return renderFlat(kv, descriptions(typeOf[T]()), renderKeyLine), nil
	case DumpEnv:
		prefix := o.envPrefix
		over := envOverrides(typeOf[T]())
		return renderFlat(kv, descriptions(typeOf[T]()), func(k, v string) string {
			if name, ok := over[k]; ok {
				return name + "=" + envValue(v)
			}
			return prefix + envName(k) + "=" + envValue(v)
		}), nil
	case DumpJSON:
		b, err := json.MarshalIndent(unflatten(kv), "", "  ")
		if err != nil {
			return "", fmt.Errorf("config: dump json: %w", err)
		}
		return string(b) + "\n", nil
	case DumpYAML:
		b, err := yaml.Marshal(unflatten(kv))
		if err != nil {
			return "", fmt.Errorf("config: dump yaml: %w", err)
		}
		return string(b), nil
	case DumpTOML:
		b, err := toml.Marshal(unflatten(kv))
		if err != nil {
			return "", fmt.Errorf("config: dump toml: %w", err)
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("config: unknown dump format %q", format)
	}
}

// DumpValues — Dump без описаний (когда тип конфигурации не под рукой или
// комментарии не нужны).
func DumpValues(cfg *Config, format DumpFormat, opts ...DumpOption) (string, error) {
	return Dump[struct{}](cfg, format, opts...)
}

// dumpPairs собирает пары текущей секции cfg: фильтрует по префиксу секции,
// срезает его и маскирует ключи из redact.
func dumpPairs(cfg *Config, o dumpOptions) map[string]string {
	out := map[string]string{}
	for k, v := range cfg.m.All() {
		if cfg.prefix != "" {
			if !strings.HasPrefix(k, cfg.prefix+":") {
				continue
			}
			k = k[len(cfg.prefix)+1:]
		}
		for _, r := range o.redact {
			if k == r || strings.HasPrefix(k, r+":") {
				v = "***"
				break
			}
		}
		out[k] = v
	}
	return out
}

// descriptions строит индекс "нормализованный ключ -> описание" из тегов
// description/usage типа t.
func descriptions(t reflect.Type) map[string]string {
	out := map[string]string{}
	for _, e := range bind.Describe(t) {
		if e.Description != "" {
			out[flat.Normalize(e.Key)] = e.Description
		}
	}
	return out
}

// describeKey ищет описание ключа: сперва точное совпадение, затем с
// заменой числовых сегментов на плейсхолдер "n" (элементы срезов в
// Describe значатся как "servers:N:host").
func describeKey(desc map[string]string, key string) string {
	if d, ok := desc[key]; ok {
		return d
	}
	segs := strings.Split(key, ":")
	changed := false
	for i, s := range segs {
		if _, err := strconv.Atoi(s); err == nil {
			segs[i] = "n"
			changed = true
		}
	}
	if changed {
		return desc[strings.Join(segs, ":")]
	}
	return ""
}

// renderFlat печатает пары в сортированном порядке, предваряя каждую
// комментарием-описанием, если оно есть.
func renderFlat(kv, desc map[string]string, line func(k, v string) string) string {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		if d := describeKey(desc, k); d != "" {
			b.WriteString("# " + d + "\n")
		}
		b.WriteString(line(k, kv[k]) + "\n")
	}
	return b.String()
}

func renderKeyLine(k, v string) string { return k + " = " + v }

// envName превращает ключ конфигурации в имя переменной среды:
// "database:host" -> "DATABASE__HOST"; недопустимые символы заменяются "_".
func envName(key string) string {
	name := strings.ToUpper(strings.ReplaceAll(key, ":", "__"))
	return strings.Map(func(r rune) rune {
		switch {
		case r == '_', r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			return r
		default:
			return '_'
		}
	}, name)
}

// envValue квотит значение, если без кавычек оно прочитается неоднозначно.
// Вывод совместим с DotEnvFile.
func envValue(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n\r\"'#\\") {
		return v
	}
	if v == "" {
		return ""
	}
	return strconv.Quote(v)
}

// unflatten восстанавливает дерево из плоских ключей: сегменты через ":",
// узлы, у которых все дети числовые, становятся массивами (по возрастанию
// индексов, дыры схлопываются). При конфликте "значение и секция под одним
// ключом" побеждает секция.
func unflatten(kv map[string]string) any {
	root := map[string]any{}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		segs := strings.Split(k, ":")
		node := root
		for _, s := range segs[:len(segs)-1] {
			child, ok := node[s].(map[string]any)
			if !ok {
				child = map[string]any{}
				node[s] = child
			}
			node = child
		}
		last := segs[len(segs)-1]
		if _, isSection := node[last].(map[string]any); !isSection {
			node[last] = kv[k]
		}
	}
	return collapseArrays(root)
}

func collapseArrays(node any) any {
	m, ok := node.(map[string]any)
	if !ok {
		return node
	}
	idx := make([]int, 0, len(m))
	allInt := len(m) > 0
	for k := range m {
		n, err := strconv.Atoi(k)
		if err != nil || n < 0 {
			allInt = false
			break
		}
		idx = append(idx, n)
	}
	if allInt {
		sort.Ints(idx)
		out := make([]any, 0, len(idx))
		for _, i := range idx {
			out = append(out, collapseArrays(m[strconv.Itoa(i)]))
		}
		return out
	}
	for k, v := range m {
		m[k] = collapseArrays(v)
	}
	return m
}
