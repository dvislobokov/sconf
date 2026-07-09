// Package flat реализует плоскую модель конфигурации: любое дерево
// (результат разбора JSON/YAML/TOML, переменные среды, аргументы) сводится
// к парам "путь -> строка" с разделителем ":" (например "servers:0:host").
package flat

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Sep — разделитель сегментов пути.
const Sep = ":"

// Combine соединяет префикс и сегмент разделителем. Пустой сегмент
// возвращает префикс без изменений (удобно для Bind по корню секции).
func Combine(prefix, segment string) string {
	switch {
	case segment == "":
		return prefix
	case prefix == "":
		return segment
	default:
		return prefix + Sep + segment
	}
}

// Normalize приводит ключ к каноничному виду. Ключи конфигурации
// регистронезависимы (как в Microsoft.Extensions.Configuration),
// поэтому храним и сравниваем их в нижнем регистре.
func Normalize(key string) string {
	return strings.ToLower(key)
}

// Flatten рекурсивно раскладывает произвольное значение в dst.
// Ключи сохраняют исходный регистр — нормализация выполняется при записи в Map.
func Flatten(dst map[string]string, prefix string, value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		for k, val := range v {
			Flatten(dst, Combine(prefix, k), val)
		}
	case map[interface{}]interface{}: // старые YAML-парсеры
		for k, val := range v {
			Flatten(dst, Combine(prefix, fmt.Sprintf("%v", k)), val)
		}
	case []interface{}:
		for i, val := range v {
			Flatten(dst, Combine(prefix, strconv.Itoa(i)), val)
		}
	default:
		if prefix == "" {
			return
		}
		dst[prefix] = ScalarToString(v)
	}
}

// ScalarToString конвертирует скаляр в строку без потери точности для чисел
// (в отличие от fmt "%v", который для больших float уходит в экспоненту).
func ScalarToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}
