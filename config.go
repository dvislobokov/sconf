package sconf

import (
	"strconv"
	"strings"

	"github.com/dvislobokov/sconf/internal/flat"
)

// Config — итоговая объединённая конфигурация. Регистронезависима,
// безопасна для конкурентного чтения.
type Config struct {
	m      *flat.Map
	prefix string // нормализованный префикс текущей секции ("" — корень)
}

func (c *Config) key(k string) string {
	return flat.Combine(c.prefix, k)
}

// Get возвращает строковое значение по ключу и признак наличия.
// Ключ иерархический: "database:host".
func (c *Config) Get(key string) (string, bool) {
	return c.m.Get(c.key(key))
}

// GetString возвращает строку или "" если ключа нет.
func (c *Config) GetString(key string) string {
	v, _ := c.Get(key)
	return v
}

// GetInt возвращает целое или def, если ключа нет либо он не парсится.
func (c *Config) GetInt(key string, def int) int {
	v, ok := c.Get(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

// GetBool возвращает булево или def.
func (c *Config) GetBool(key string, def bool) bool {
	v, ok := c.Get(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}

// Exists сообщает, есть ли значение или вложенная секция по ключу.
func (c *Config) Exists(key string) bool {
	return c.m.Has(c.key(key))
}

// Section возвращает вложенную секцию. Секция разделяет данные с родителем,
// но ограничена префиксом ключа.
func (c *Config) Section(key string) *Config {
	return &Config{m: c.m, prefix: flat.Normalize(c.key(key))}
}

// GetChildren возвращает имена непосредственных дочерних сегментов по ключу
// (нормализованные, отсортированные). Пустой key — дети текущей секции.
func (c *Config) GetChildren(key string) []string {
	return c.m.ChildSegments(c.key(key))
}
