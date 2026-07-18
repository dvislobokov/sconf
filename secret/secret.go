// Package secret описывает типы полей конфигурации, значения которых берутся
// из хранилища секретов (Vault). Пакет намеренно лёгкий — из внешнего только
// парсеры YAML/TOML (для разбора кредов из текстового поля KV): сами типы лишь
// описывают, ЧТО нужно достать (SecretRequest) и как разложить ответ (Apply).
// Механику похода в Vault реализует внутренний пакет sconf/internal/vault,
// встроенный в ядро sconf.
//
// В YAML/JSON-конфиге поле секрета задаётся строкой-путём:
//
//	db_cred: database/creds/app-role
//
// либо путём с дополнительными параметрами в стиле query-строки:
//
//	web_cert: pki/issue/web?common_name=app.example.com&ttl=24h
//
// sconf.Load после бинда обходит структуру, находит поля-Resolvable и
// заполняет их из Vault автоматически.
//
// Без Vault значение можно задать в конфиге напрямую: вложенной секцией с теми
// же полями, что вернул бы Vault (для Value — единственное поле value), либо
// однострочно через префикс "plain:" (см. PlainPrefix). Вложенная секция
// побеждает путь из другого слоя конфигурации.
package secret

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// reservedParams — параметры пути, управляющие резолвером/обновлением, а не
// передаваемые в Vault как тело запроса.
var reservedParams = map[string]bool{
	"refresh":        true,
	"field":          true,
	"username_field": true,
	"password_field": true,
}

// Method — вид обращения к Vault для получения секрета.
type Method int

const (
	// Read — чтение секрета (GET logical): динамические креды БД, AD, KV, userpass.
	Read Method = iota
	// Write — запись с параметрами (PUT logical): например выпуск сертификата
	// движком pki (pki/issue/<role>).
	Write
)

// Request описывает обращение к Vault, необходимое для получения секрета.
// Path — путь от корня mount'а (например "database/creds/app"); резолвер
// может дополнительно применить префикс VAULT_MOUNTPATH. Data передаётся при
// Method == Write как тело запроса. Refresh — явный интервал обновления из
// параметра пути ?refresh= (0 — не задан, резолвер выберет политику по умолчанию).
type Request struct {
	Method  Method
	Path    string
	Data    map[string]any
	Refresh time.Duration
}

// Resolvable реализуется типами полей секретов. Резолвер (sconf) для
// каждого такого поля выполняет SecretRequest, а результат передаёт в Apply.
// Интерфейс с указательным получателем — поля должны быть адресуемыми
// (что верно после bind в *T).
type Resolvable interface {
	// SecretRequest возвращает описание того, что и как достать из Vault.
	SecretRequest() Request
	// Apply раскладывает данные ответа Vault (содержимое data) в поля типа.
	Apply(data map[string]any) error
}

// Refreshable реализуется секретами, поддерживающими фоновое обновление.
// Все типы этого пакета его реализуют (через встроенный refreshState).
type Refreshable interface {
	Resolvable
	// SetRefresh запоминает интервал до следующего обновления (вычисляется
	// резолвером из ответа Vault). Потокобезопасно.
	SetRefresh(d time.Duration)
	// Refresh возвращает текущий интервал до обновления (0 — не обновлять).
	Refresh() time.Duration
}

// refreshState — встраиваемое потокобезопасное хранилище интервала обновления.
type refreshState struct {
	intervalNanos atomic.Int64
}

func (r *refreshState) SetRefresh(d time.Duration) { r.intervalNanos.Store(int64(d)) }
func (r *refreshState) Refresh() time.Duration     { return time.Duration(r.intervalNanos.Load()) }

// refreshParam извлекает интервал из параметра ?refresh= (0, если не задан или
// не парсится).
func (r ref) refreshParam() time.Duration {
	v := r.params["refresh"]
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// PlainPrefix помечает значение секрета, заданное в конфиге напрямую, без
// Vault: "plain:<значение>". Для UserPass/KV/Cert значение — текст-отображение
// в формате JSON, YAML или TOML (те же поля, что вернул бы Vault); для Value —
// готовая строка как есть. Такие секреты не требуют настроенного окружения
// Vault и не обновляются в фоне. Удобно для локальной разработки и стендов
// без Vault; помните, что секрет при этом лежит в конфигурации открытым
// текстом (и попадает в Dump).
const PlainPrefix = "plain:"

// plainPayload возвращает значение после PlainPrefix, если префикс есть.
func plainPayload(value string) (string, bool) {
	return strings.CutPrefix(strings.TrimSpace(value), PlainPrefix)
}

// decodeMap разбирает текстовое значение секрета в отображение ключ→значение,
// пробуя форматы по порядку: JSON, YAML, TOML. Возвращает ошибку, если текст
// не является отображением ни в одном из форматов.
func decodeMap(text string) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("secret: field value is empty")
	}
	b := []byte(text)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err == nil && m != nil {
		return m, nil
	}
	m = nil
	if err := yaml.Unmarshal(b, &m); err == nil && m != nil {
		return m, nil
	}
	m = nil
	if err := toml.Unmarshal(b, &m); err == nil && m != nil {
		return m, nil
	}
	return nil, fmt.Errorf("secret: value is not a JSON, YAML or TOML mapping")
}

// ref — разобранное строковое значение поля секрета: путь и доп. параметры.
type ref struct {
	path   string
	params map[string]string
}

// parseRef разбирает строку конфигурации вида "path" либо
// "path?k1=v1&k2=v2" в ref. Пустой путь — ошибка.
func parseRef(value string) (ref, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ref{}, fmt.Errorf("secret: empty path")
	}

	path, rawQuery, hasQuery := strings.Cut(value, "?")
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ref{}, fmt.Errorf("secret: empty path in %q", value)
	}

	r := ref{path: path}
	if hasQuery && rawQuery != "" {
		q, err := url.ParseQuery(rawQuery)
		if err != nil {
			return ref{}, fmt.Errorf("secret: invalid params in %q: %w", value, err)
		}
		r.params = make(map[string]string, len(q))
		for k := range q {
			r.params[k] = q.Get(k)
		}
	}
	return r, nil
}

// dataMap копирует params в map[string]any (тело для Write-запроса), исключая
// управляющие параметры (refresh и т.п.), которые не предназначены для Vault.
func (r ref) dataMap() map[string]any {
	out := make(map[string]any, len(r.params))
	for k, v := range r.params {
		if reservedParams[k] {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// asString приводит значение из ответа Vault к строке. Vault отдаёт данные
// как JSON, поэтому числа приходят как json.Number/float64, строки как string.
func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}

// asStrings приводит значение-массив (например ca_chain) к []string.
func asStrings(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, asString(e))
		}
		return out
	default:
		return []string{asString(x)}
	}
}
