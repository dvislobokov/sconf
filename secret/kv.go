package secret

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
)

// KV — произвольный секрет из движка Key/Value, прочитанный по ПОЛНОМУ пути.
// В KV может лежать что угодно, поэтому данные отдаются как набор пар
// ключ→строка. Подходит, когда нужно достать сразу все поля секрета.
//
//	type Config struct {
//	    Extra secret.KV `yaml:"extra"`
//	}
//
//	# appsettings.yaml — для KV v2 путь включает "data"
//	extra: secret/data/myapp
//
// Затем: cfg.Extra.Get("api_key") либо перебор cfg.Extra.Values().
//
// Обёртка KV v2 (data/metadata) снимается автоматически. Значения читаются
// потокобезопасно и могут обновляться в фоне (по умолчанию раз в 30 минут,
// переопределяется ?refresh=).
type KV struct {
	refreshState
	ref    ref
	values atomic.Pointer[map[string]string]
}

// UnmarshalConfig принимает полный путь до KV-секрета.
func (k *KV) UnmarshalConfig(value string) error {
	r, err := parseRef(value)
	if err != nil {
		return err
	}
	k.ref = r
	return nil
}

// SecretRequest сообщает резолверу прочитать секрет по полному пути.
func (k *KV) SecretRequest() Request {
	return Request{Method: Read, Path: k.ref.path, Refresh: k.ref.refreshParam()}
}

// Apply раскладывает все поля секрета в снапшот (значения приводятся к строке).
func (k *KV) Apply(data map[string]any) error {
	d := KVFields(data)
	m := make(map[string]string, len(d))
	for key, v := range d {
		m[key] = asString(v)
	}
	k.values.Store(&m)
	return nil
}

// Get возвращает значение поля секрета (пустая строка, если поля нет).
func (k *KV) Get(key string) string {
	if m := k.values.Load(); m != nil {
		return (*m)[key]
	}
	return ""
}

// Values возвращает снапшот всех полей секрета. Карту нельзя изменять — при
// фоновом обновлении она заменяется целиком.
func (k *KV) Values() map[string]string {
	if m := k.values.Load(); m != nil {
		return *m
	}
	return nil
}

// Resolved сообщает, был ли секрет получен из Vault.
func (k *KV) Resolved() bool { return k.values.Load() != nil }

// Path возвращает полный путь до секрета, как он задан в конфиге.
func (k *KV) Path() string { return k.ref.path }

// Value — одно поле KV-секрета, прочитанное по ПОЛНОМУ пути. Имя поля задаётся
// параметром field; если он опущен и в секрете ровно одно поле, берётся оно.
//
//	type Config struct {
//	    APIKey secret.Value `yaml:"api_key"`
//	}
//
//	# appsettings.yaml
//	api_key: secret/data/myapp?field=api_key
//
// Затем: cfg.APIKey.Get(). Значение читается потокобезопасно и может обновляться
// в фоне (по умолчанию раз в 30 минут, переопределяется ?refresh=).
type Value struct {
	refreshState
	ref   ref
	value atomic.Pointer[string]
}

// UnmarshalConfig принимает полный путь до секрета и (опционально) имя поля.
func (v *Value) UnmarshalConfig(value string) error {
	r, err := parseRef(value)
	if err != nil {
		return err
	}
	v.ref = r
	return nil
}

// SecretRequest сообщает резолверу прочитать секрет по полному пути.
func (v *Value) SecretRequest() Request {
	return Request{Method: Read, Path: v.ref.path, Refresh: v.ref.refreshParam()}
}

// Apply извлекает одно поле. Имя берётся из параметра field; если он не задан,
// а в секрете единственное поле — берётся оно, иначе возвращается ошибка со
// списком доступных полей.
func (v *Value) Apply(data map[string]any) error {
	d := KVFields(data)
	field := v.ref.params["field"]
	if field == "" {
		if len(d) != 1 {
			return fmt.Errorf("secret: %q: specify ?field= (available: %s)",
				v.ref.path, strings.Join(sortedKeys(d), ", "))
		}
		for k := range d {
			field = k
		}
	}
	raw, ok := d[field]
	if !ok {
		return fmt.Errorf("secret: %q: no field %q (available: %s)",
			v.ref.path, field, strings.Join(sortedKeys(d), ", "))
	}
	s := asString(raw)
	v.value.Store(&s)
	return nil
}

// Get возвращает значение поля (потокобезопасно).
func (v *Value) Get() string {
	if s := v.value.Load(); s != nil {
		return *s
	}
	return ""
}

// Resolved сообщает, был ли секрет получен из Vault.
func (v *Value) Resolved() bool { return v.value.Load() != nil }

// Path возвращает полный путь до секрета, как он задан в конфиге.
func (v *Value) Path() string { return v.ref.path }

// KVFields снимает обёртку KV v2 и возвращает сами поля секрета. Ответ KV v2
// имеет вид {"data": {...поля...}, "metadata": {...}} — тогда возвращаются
// внутренние поля. Для KV v1 (и прочих движков) ответ используется как есть.
// Экспортируется для переиспользования Vault-провайдером конфигурации.
func KVFields(data map[string]any) map[string]any {
	inner, okData := data["data"].(map[string]any)
	if _, okMeta := data["metadata"]; okData && okMeta {
		return inner
	}
	return data
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
