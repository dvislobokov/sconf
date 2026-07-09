package flat

import (
	"sort"
	"strconv"
	"strings"
)

// Map — плоская модель конфигурации. Ключи хранятся нормализованными
// (нижний регистр); исходное написание сохраняется отдельно для сообщений
// об ошибках. Map не потокобезопасна на запись, но безопасна на чтение
// после сборки.
type Map struct {
	values map[string]string // нормализованный ключ -> значение
	orig   map[string]string // нормализованный ключ -> исходное написание
}

// New создаёт пустую Map.
func New() *Map {
	return &Map{
		values: map[string]string{},
		orig:   map[string]string{},
	}
}

// Set записывает значение, нормализуя ключ и запоминая исходное написание.
func (m *Map) Set(key, value string) {
	n := Normalize(key)
	m.values[n] = value
	m.orig[n] = key
}

// SetAll накладывает набор значений поверх текущих (last-wins per key).
func (m *Map) SetAll(kv map[string]string) {
	for k, v := range kv {
		m.Set(k, v)
	}
}

// Get возвращает значение по ключу и признак наличия.
func (m *Map) Get(key string) (string, bool) {
	v, ok := m.values[Normalize(key)]
	return v, ok
}

// Orig возвращает исходное написание ключа (для ошибок). Если ключ —
// промежуточный сегмент пути без собственного значения, возвращается вход.
func (m *Map) Orig(key string) string {
	if o, ok := m.orig[Normalize(key)]; ok {
		return o
	}
	return key
}

// Has сообщает, есть ли значение или вложенная секция по ключу.
func (m *Map) Has(key string) bool {
	n := Normalize(key)
	if _, ok := m.values[n]; ok {
		return true
	}
	prefix := n + Sep
	for k := range m.values {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// ChildSegments возвращает уникальные непосредственные дочерние сегменты
// относительно prefix, отсортированные лексикографически.
func (m *Map) ChildSegments(prefix string) []string {
	lp := Normalize(prefix)
	seen := map[string]struct{}{}
	var out []string
	for k := range m.values {
		var rest string
		switch {
		case lp == "":
			rest = k
		case strings.HasPrefix(k, lp+Sep):
			rest = k[len(lp)+1:]
		default:
			continue
		}
		seg := rest
		if i := strings.IndexByte(rest, ':'); i >= 0 {
			seg = rest[:i]
		}
		if _, ok := seen[seg]; !ok {
			seen[seg] = struct{}{}
			out = append(out, seg)
		}
	}
	sort.Strings(out)
	return out
}

// ChildIndices возвращает числовые дочерние сегменты, отсортированные по
// возрастанию. Дыры в индексах допустимы — вызывающий код схлопывает
// элементы по порядку.
func (m *Map) ChildIndices(prefix string) []int {
	var idx []int
	for _, seg := range m.ChildSegments(prefix) {
		if i, err := strconv.Atoi(seg); err == nil && i >= 0 {
			idx = append(idx, i)
		}
	}
	sort.Ints(idx)
	return idx
}

// Len возвращает число листовых значений.
func (m *Map) Len() int { return len(m.values) }
