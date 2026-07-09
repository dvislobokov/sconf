package bind

import (
	"fmt"
	"reflect"
	"strings"
)

// Entry описывает один конфигурационный ключ для генерации usage.
type Entry struct {
	Key         string   // полный путь через ":"
	Type        string   // человекочитаемый тип
	Default     string   // значение по умолчанию (тег default)
	HasDefault  bool     // задан ли default
	Enum        []string // допустимые значения (тег enum)
	Description string   // описание (тег description или usage)
}

// Describe обходит тип t и возвращает список конфигурационных ключей.
// t может быть структурой или указателем на неё.
func Describe(t reflect.Type) []Entry {
	t = derefType(t)
	var out []Entry
	if t != nil && t.Kind() == reflect.Struct && t != timeType {
		collectStruct(t, "", &out)
	}
	return out
}

// Usage форматирует человекочитаемую справку по типу t.
func Usage(t reflect.Type) string {
	entries := Describe(t)
	var b strings.Builder
	b.WriteString("Options:\n")
	if len(entries) == 0 {
		return b.String()
	}

	// Ширина колонки ключа для выравнивания.
	keyW := 0
	for _, e := range entries {
		if n := len(e.Key) + 2; n > keyW { // +2 для "--"
			keyW = n
		}
	}

	for _, e := range entries {
		fmt.Fprintf(&b, "  %-*s  %s", keyW, "--"+e.Key, e.Type)
		if len(e.Enum) > 0 {
			fmt.Fprintf(&b, "  {%s}", strings.Join(e.Enum, "|"))
		}
		if e.HasDefault {
			fmt.Fprintf(&b, "  (default %q)", e.Default)
		}
		if e.Description != "" {
			fmt.Fprintf(&b, "  %s", e.Description)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func collectStruct(t reflect.Type, prefix string, out *[]Entry) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, skip := fieldName(field)
		if skip {
			continue
		}

		// Встроенная структура без явного имени — на том же уровне.
		if field.Anonymous && !hasExplicitTag(field) && field.Type.Kind() == reflect.Struct {
			collectStruct(field.Type, prefix, out)
			continue
		}

		key := joinKey(prefix, name)
		ft := derefType(field.Type)

		switch {
		case isScalarType(ft):
			*out = append(*out, leafEntry(key, ft, field))

		case ft.Kind() == reflect.Slice:
			et := derefType(ft.Elem())
			if isScalarType(et) {
				*out = append(*out, leafEntry(key, ft, field))
			} else if et.Kind() == reflect.Struct {
				collectStruct(et, joinKey(key, "N"), out)
			}

		case ft.Kind() == reflect.Map:
			et := derefType(ft.Elem())
			if isScalarType(et) {
				*out = append(*out, leafEntry(key, ft, field))
			} else if et.Kind() == reflect.Struct {
				collectStruct(et, joinKey(key, "<key>"), out)
			}

		case ft.Kind() == reflect.Struct:
			collectStruct(ft, key, out)
		}
	}
}

func leafEntry(key string, t reflect.Type, field reflect.StructField) Entry {
	e := Entry{
		Key:         key,
		Type:        typeName(t),
		Enum:        splitList(field.Tag.Get("enum")),
		Description: description(field),
	}
	if d, ok := field.Tag.Lookup("default"); ok {
		e.Default, e.HasDefault = d, true
	}
	return e
}

// description берёт описание из тега description, иначе из usage.
func description(field reflect.StructField) string {
	if v, ok := field.Tag.Lookup("description"); ok {
		return v
	}
	return field.Tag.Get("usage")
}

// isScalarType сообщает, представляется ли тип единичным значением
// (примитив, duration/time, либо реализует Unmarshaler).
func isScalarType(t reflect.Type) bool {
	if t == nil {
		return false
	}
	if t == durationType || t == timeType {
		return true
	}
	if reflect.PtrTo(t).Implements(unmarshalerType) {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func typeName(t reflect.Type) string {
	switch {
	case t == durationType:
		return "duration"
	case t == timeType:
		return "datetime"
	case t.Kind() == reflect.Slice:
		return "[]" + typeName(derefType(t.Elem()))
	case t.Kind() == reflect.Map:
		return fmt.Sprintf("map[%s]%s", t.Key().Kind(), typeName(derefType(t.Elem())))
	default:
		return t.Kind().String()
	}
}

func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func joinKey(prefix, seg string) string {
	if prefix == "" {
		return seg
	}
	return prefix + ":" + seg
}
