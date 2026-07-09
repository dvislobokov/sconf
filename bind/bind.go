// Package bind реализует reflection-биндер: заполняет структуры, срезы,
// map и примитивы из плоской модели конфигурации. Поддерживает теги
// default (значение по умолчанию) и enum (список допустимых значений).
package bind

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/dvislobokov/sconf/internal/flat"
)

// ErrBindType возвращается (через %w), когда значение нельзя привести
// к целевому типу.
var ErrBindType = errors.New("config: cannot bind value to type")

// ErrEnum возвращается (через %w), когда значение не входит в список enum.
var ErrEnum = errors.New("config: value not allowed")

// Unmarshaler позволяет типу самому разобрать своё строковое представление.
// Проверяется до reflection.
type Unmarshaler interface {
	UnmarshalConfig(value string) error
}

// Validator, если реализован целевым типом, вызывается после успешного бинда.
type Validator interface {
	Validate() error
}

var (
	durationType    = reflect.TypeOf(time.Duration(0))
	timeType        = reflect.TypeOf(time.Time{})
	unmarshalerType = reflect.TypeOf((*Unmarshaler)(nil)).Elem()
)

// tagKeys — теги, из которых берётся имя ключа, в порядке приоритета.
var tagKeys = [...]string{"json", "yaml", "toml", "name"}

// fieldMeta — метаданные поля, влияющие на разбор скаляра.
type fieldMeta struct {
	def    string
	hasDef bool
	enum   []string
}

// Bind заполняет target значениями из m по пути prefix. target обязан быть
// ненулевым указателем — иначе это программная ошибка использования (panic).
func Bind(m *flat.Map, prefix string, target interface{}) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		panic(fmt.Sprintf("config: Bind requires a non-nil pointer, got %T", target))
	}
	return bindValue(m, prefix, rv.Elem(), nil)
}

func bindValue(m *flat.Map, path string, rv reflect.Value, meta *fieldMeta) error {
	// Кастомный Unmarshaler имеет приоритет над reflection.
	if rv.CanAddr() {
		if u, ok := rv.Addr().Interface().(Unmarshaler); ok {
			s, has, err := effectiveValue(m, path, meta)
			if err != nil {
				return err
			}
			if has {
				if err := u.UnmarshalConfig(s); err != nil {
					return bindErr(path, s, rv.Type(), err)
				}
			}
			return validate(path, rv)
		}
	}

	switch rv.Kind() {
	case reflect.Ptr:
		if !m.Has(path) && (meta == nil || !meta.hasDef) {
			return nil // нет данных и нет дефолта — указатель не аллоцируем
		}
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		return bindValue(m, path, rv.Elem(), meta)

	case reflect.Struct:
		if rv.Type() == timeType {
			return bindScalar(m, path, rv, meta)
		}
		if err := bindStruct(m, path, rv); err != nil {
			return err
		}
		return validate(path, rv)

	case reflect.Slice:
		if err := bindSlice(m, path, rv); err != nil {
			return err
		}
		return validate(path, rv)

	case reflect.Map:
		if err := bindMap(m, path, rv); err != nil {
			return err
		}
		return validate(path, rv)

	default:
		if err := bindScalar(m, path, rv, meta); err != nil {
			return err
		}
		return validate(path, rv)
	}
}

func bindStruct(m *flat.Map, path string, rv reflect.Value) error {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" { // неэкспортируемое поле
			continue
		}

		name, skip := fieldName(field)
		if skip {
			continue
		}

		// Встроенная (embedded) структура без явного имени — на том же уровне.
		if field.Anonymous && !hasExplicitTag(field) && field.Type.Kind() == reflect.Struct {
			if err := bindValue(m, path, rv.Field(i), nil); err != nil {
				return err
			}
			continue
		}

		key := flat.Combine(path, name)
		if err := bindValue(m, key, rv.Field(i), metaOf(field)); err != nil {
			return err
		}
	}
	return nil
}

func bindSlice(m *flat.Map, path string, rv reflect.Value) error {
	idx := m.ChildIndices(path)
	if len(idx) == 0 {
		return nil
	}
	// Дыры схлопываются: элемент j берётся из индекса idx[j].
	slice := reflect.MakeSlice(rv.Type(), len(idx), len(idx))
	for j, i := range idx {
		if err := bindValue(m, flat.Combine(path, strconv.Itoa(i)), slice.Index(j), nil); err != nil {
			return err
		}
	}
	rv.Set(slice)
	return nil
}

func bindMap(m *flat.Map, path string, rv reflect.Value) error {
	keys := m.ChildSegments(path)
	if len(keys) == 0 {
		return nil
	}
	if rv.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("config: map key must be string, got %s: %w", rv.Type(), ErrBindType)
	}
	out := reflect.MakeMapWithSize(rv.Type(), len(keys))
	for _, k := range keys {
		elem := reflect.New(rv.Type().Elem()).Elem()
		if err := bindValue(m, flat.Combine(path, k), elem, nil); err != nil {
			return err
		}
		out.SetMapIndex(reflect.ValueOf(k).Convert(rv.Type().Key()), elem)
	}
	rv.Set(out)
	return nil
}

func bindScalar(m *flat.Map, path string, rv reflect.Value, meta *fieldMeta) error {
	s, has, err := effectiveValue(m, path, meta)
	if err != nil {
		return err
	}
	if !has {
		return nil
	}
	if err := setScalar(rv, s); err != nil {
		return bindErr(path, s, rv.Type(), err)
	}
	return nil
}

// effectiveValue возвращает значение из источников либо, при его отсутствии,
// из тега default. Если задан enum, значение валидируется и приводится к
// каноничному написанию из списка.
func effectiveValue(m *flat.Map, path string, meta *fieldMeta) (string, bool, error) {
	s, ok := m.Get(path)
	if !ok {
		if meta == nil || !meta.hasDef {
			return "", false, nil
		}
		s = meta.def
	}
	if meta != nil && len(meta.enum) > 0 {
		canon, valid := matchEnum(strings.TrimSpace(s), meta.enum)
		if !valid {
			return "", false, fmt.Errorf("config: %q = %q: %w (allowed: %s)",
				path, s, ErrEnum, strings.Join(meta.enum, ", "))
		}
		s = canon
	}
	return s, true, nil
}

// matchEnum сравнивает значение с допустимыми регистронезависимо и возвращает
// каноничное написание из списка.
func matchEnum(value string, enum []string) (string, bool) {
	for _, e := range enum {
		if strings.EqualFold(value, e) {
			return e, true
		}
	}
	return "", false
}

func setScalar(rv reflect.Value, s string) error {
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		return setScalar(rv.Elem(), s)
	}

	s = strings.TrimSpace(s)

	switch rv.Type() {
	case durationType:
		d, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		rv.SetInt(int64(d))
		return nil
	case timeType:
		t, err := parseTime(s)
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(t))
		return nil
	}

	switch rv.Kind() {
	case reflect.String:
		rv.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		rv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		rv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		rv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		rv.SetFloat(f)
	default:
		return fmt.Errorf("unsupported kind %s", rv.Kind())
	}
	return nil
}

func validate(path string, rv reflect.Value) error {
	if !rv.CanAddr() {
		return nil
	}
	if v, ok := rv.Addr().Interface().(Validator); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("config: validate %q: %w", path, err)
		}
	}
	return nil
}

func metaOf(field reflect.StructField) *fieldMeta {
	m := fieldMeta{}
	if d, ok := field.Tag.Lookup("default"); ok {
		m.def, m.hasDef = d, true
	}
	m.enum = splitList(field.Tag.Get("enum"))
	if !m.hasDef && len(m.enum) == 0 {
		return nil
	}
	return &m
}

// fieldName определяет имя ключа поля из тегов json/yaml/toml/name (в этом
// порядке), либо из имени поля. Возвращает skip=true для тега "-".
func fieldName(field reflect.StructField) (name string, skip bool) {
	for _, tag := range tagKeys {
		v, ok := field.Tag.Lookup(tag)
		if !ok {
			continue
		}
		v = strings.Split(v, ",")[0] // отсекаем ",omitempty" и т.п.
		if v == "-" {
			return "", true
		}
		if v != "" {
			return v, false
		}
	}
	return field.Name, false
}

func hasExplicitTag(field reflect.StructField) bool {
	for _, tag := range tagKeys {
		if v, ok := field.Tag.Lookup(tag); ok {
			if name := strings.Split(v, ",")[0]; name != "" {
				return true
			}
		}
	}
	return false
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func bindErr(path, value string, t reflect.Type, cause error) error {
	msg := fmt.Sprintf("config: cannot bind %q (value %q) to %s", path, value, t)
	if cause != nil {
		return fmt.Errorf("%s: %w (%v)", msg, ErrBindType, cause)
	}
	return fmt.Errorf("%s: %w", msg, ErrBindType)
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", s)
}
