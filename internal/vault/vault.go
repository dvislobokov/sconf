// Package vault — внутренняя реализация работы sconf с HashiCorp Vault
// (github.com/hashicorp/vault-client-go). Ядро sconf вызывает Resolve после
// бинда конфигурации и Watch для фонового обновления секретов; со стороны
// приклада не требуется ни отдельного импорта, ни регистрации.
//
// После бинда sconf.Load обходит структуру, находит поля-секреты (типы,
// реализующие secret.Resolvable) и заполняет их из Vault. Если полей-секретов
// нет, Vault не задействуется вовсе. Если поля есть, но окружение Vault не
// настроено (нет VAULT_ADDR и т.п.), Load возвращает ошибку (ErrNotConfigured).
//
// Конфигурация подключения берётся из переменных среды — см. loadConfig.
package vault

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/dvislobokov/sconf/secret"
	vault "github.com/hashicorp/vault-client-go"
)

// resolveOptions — настройки Resolve.
type resolveOptions struct {
	wait waitOptions
}

// ResolveOption настраивает поведение Resolve.
type ResolveOption func(*resolveOptions)

// WithWait задаёт суммарное время ожидания доступности Vault при загрузке:
// временные ошибки (сеть, 5xx) повторяются, пока не истечёт timeout.
// По умолчанию ожидание выключено. Переменная среды VAULT_WAIT имеет приоритет.
func WithWait(timeout time.Duration) ResolveOption {
	return func(o *resolveOptions) { o.wait.timeout = timeout }
}

// WithWaitInterval задаёт паузу между попытками ожидания (по умолчанию 2s).
// Переменная среды VAULT_WAIT_INTERVAL имеет приоритет.
func WithWaitInterval(d time.Duration) ResolveOption {
	return func(o *resolveOptions) { o.wait.interval = d }
}

// Resolve обходит target (указатель на конфигурацию), находит поля-секреты и
// заполняет их из Vault. Если секретов нет — возвращает nil, ничего не требуя
// от окружения. При включённом ожидании (WithWait или VAULT_WAIT) временные
// ошибки — Vault ещё недоступен, например за неготовым sidecar-прокси —
// повторяются, пока не истечёт лимит.
func Resolve(ctx context.Context, target any, opts ...ResolveOption) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return nil
	}

	found, writebacks := collect(rv.Elem())
	found = needsVault(found)
	if len(found) == 0 {
		return nil
	}

	var ro resolveOptions
	for _, op := range opts {
		op(&ro)
	}
	wait, err := loadWait(ro.wait)
	if err != nil {
		return err
	}

	err = waitReady(ctx, wait, func() error {
		src, err := newStore(ctx)
		if err != nil {
			return err
		}
		for _, s := range found {
			if err := resolveOne(ctx, src, s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, wb := range writebacks {
		wb()
	}
	return nil
}

// store — источник данных секретов: Vault (боевой) либо локальный файл
// (VAULT_SECRETS_FILE) для разработки без Vault.
type store interface {
	fetch(ctx context.Context, req secret.Request) (data map[string]any, lease time.Duration, err error)
}

// newStore выбирает источник: если найден локальный файл секретов
// (VAULT_SECRETS_FILE либо vault.secrets в рабочей директории) — читаем его
// (в Vault не ходим, VAULT_ADDR не требуется); иначе — Vault.
func newStore(ctx context.Context) (store, error) {
	if path := secretsFilePath(); path != "" {
		return newFileStore(path)
	}
	client, cfg, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	return vaultStore{client: client, cfg: cfg}, nil
}

// defaultSecretsFile — имя локального файла секретов, подхватываемого из
// рабочей директории автоматически, без переменных среды.
const defaultSecretsFile = "vault.secrets"

// secretsFilePath возвращает путь до локального файла секретов: явный
// VAULT_SECRETS_FILE имеет приоритет; иначе — vault.secrets в рабочей
// директории, если такой файл существует. Пустая строка — файла нет,
// секреты читаются из Vault.
func secretsFilePath() string {
	if path := getenv("VAULT_SECRETS_FILE"); path != "" {
		return path
	}
	if fi, err := os.Stat(defaultSecretsFile); err == nil && !fi.IsDir() {
		return defaultSecretsFile
	}
	return ""
}

// needsVault отфильтровывает секреты, которым поход в Vault не нужен: значения,
// заданные в конфиге напрямую (plain:), уже применены при бинде — у них пустой
// путь и Resolved() == true. Секрет с пустым путём, но НЕ заполненный (поле
// объявлено, а в конфиге не задано), остаётся в списке — по нему resolveOne
// вернёт понятную ошибку.
func needsVault(found []secret.Resolvable) []secret.Resolvable {
	out := found[:0]
	for _, s := range found {
		if s.SecretRequest().Path == "" {
			if r, ok := s.(interface{ Resolved() bool }); ok && r.Resolved() {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// resolveOne получает данные секрета из src, раскладывает их и обновляет интервал.
func resolveOne(ctx context.Context, src store, s secret.Resolvable) error {
	req := s.SecretRequest()
	if req.Path == "" {
		return fmt.Errorf("vault: secret %T has empty path", s)
	}
	data, lease, err := src.fetch(ctx, req)
	if err != nil {
		return err
	}
	if err := s.Apply(data); err != nil {
		return fmt.Errorf("vault: apply %q: %w", req.Path, err)
	}
	if r, ok := s.(secret.Refreshable); ok {
		r.SetRefresh(refreshInterval(s, req, lease))
	}
	return nil
}

// vaultStore читает секреты из Vault.
type vaultStore struct {
	client *vault.Client
	cfg    config
}

func (v vaultStore) fetch(ctx context.Context, req secret.Request) (map[string]any, time.Duration, error) {
	path := joinPath(v.cfg.MountPath, req.Path)
	var (
		resp *vault.Response[map[string]interface{}]
		err  error
	)
	switch req.Method {
	case secret.Read:
		resp, err = v.client.Read(ctx, path)
	case secret.Write:
		resp, err = v.client.Write(ctx, path, req.Data)
	default:
		return nil, 0, fmt.Errorf("vault: unknown request method %d", req.Method)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("vault: fetch %q: %w", path, err)
	}
	if resp == nil {
		return nil, 0, fmt.Errorf("vault: fetch %q: empty response", path)
	}
	return resp.Data, leaseTTL(resp), nil
}

// dial собирает конфигурацию из окружения, создаёт клиент Vault, ставит
// namespace и аутентифицируется. Используется и резолвером, и KV-провайдером.
func dial(ctx context.Context) (*vault.Client, config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, config{}, err
	}
	client, err := newClient(cfg)
	if err != nil {
		return nil, config{}, fmt.Errorf("vault: new client: %w", err)
	}
	if cfg.Namespace != "" {
		if err := client.SetNamespace(cfg.Namespace); err != nil {
			return nil, config{}, fmt.Errorf("vault: set namespace: %w", err)
		}
	}
	if err := authenticate(ctx, client, cfg); err != nil {
		return nil, config{}, err
	}
	return client, cfg, nil
}

// defaultRefresh — интервал обновления по умолчанию для секретов, читаемых по
// расписанию (AD/DB/KV): раз в полчаса.
const defaultRefresh = 30 * time.Minute

// minRefresh — нижняя граница, чтобы не «долбить» источник при коротком lease.
const minRefresh = 10 * time.Second

// refreshInterval вычисляет интервал до следующего обновления секрета:
//
//   - явный ?refresh= из конфига имеет наивысший приоритет;
//   - для pki (Cert) — по TTL: ~70% срока действия (lease);
//   - для остальных — раз в полчаса, но раньше, если lease короче (иначе креды
//     протухнут до обновления).
//
// lease == 0 (нет аренды, например локальный файл) — берётся значение по
// умолчанию.
func refreshInterval(s secret.Resolvable, req secret.Request, lease time.Duration) time.Duration {
	if req.Refresh > 0 {
		return req.Refresh
	}
	if _, isCert := s.(*secret.Cert); isCert {
		if lease > 0 {
			return clampRefresh(scale(lease))
		}
		return defaultRefresh
	}
	d := defaultRefresh
	if lease > 0 {
		if l := scale(lease); l < d {
			d = l
		}
	}
	return clampRefresh(d)
}

// leaseTTL извлекает срок действия из ответа: сперва lease_duration, затем поле
// ttl в данных (его отдают, например, статические роли).
func leaseTTL(resp *vault.Response[map[string]interface{}]) time.Duration {
	if resp.LeaseDuration > 0 {
		return time.Duration(resp.LeaseDuration) * time.Second
	}
	if secs, ok := numField(secret.KVFields(resp.Data), "ttl"); ok && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// scale берёт ~70% срока — запас на обновление до фактического истечения.
func scale(d time.Duration) time.Duration { return d * 7 / 10 }

func clampRefresh(d time.Duration) time.Duration {
	if d < minRefresh {
		return minRefresh
	}
	return d
}

// numField пытается прочитать числовое поле (Vault отдаёт числа как json.Number
// или float64).
func numField(m map[string]any, key string) (int64, bool) {
	switch x := m[key].(type) {
	case float64:
		return int64(x), true
	case int:
		return int64(x), true
	case int64:
		return x, true
	case interface{ Int64() (int64, error) }: // json.Number
		if n, err := x.Int64(); err == nil {
			return n, true
		}
	}
	return 0, false
}

// newClient создаёт клиент Vault из конфигурации.
func newClient(c config) (*vault.Client, error) {
	opts := []vault.ClientOption{
		vault.WithAddress(c.Address),
		vault.WithRequestTimeout(c.Timeout),
	}
	if c.TLSSkip {
		opts = append(opts, vault.WithTLS(vault.TLSConfiguration{InsecureSkipVerify: true}))
	}
	rc, set, err := retryConfigFromEnv()
	if err != nil {
		return nil, err
	}
	if set {
		opts = append(opts, vault.WithRetryConfiguration(rc))
	}
	return vault.New(opts...)
}

// retryConfigFromEnv настраивает внутренние ретраи HTTP-клиента Vault
// (retryablehttp) из переменных среды. По умолчанию клиент повторяет
// запрос при 5xx/412 дважды с паузой 1–1.5s.
//
//	VAULT_MAX_RETRIES     число повторов (-1 — отключить)
//	VAULT_RETRY_WAIT_MIN  минимальная пауза между повторами
//	VAULT_RETRY_WAIT_MAX  максимальная пауза между повторами
func retryConfigFromEnv() (vault.RetryConfiguration, bool, error) {
	rc := vault.DefaultConfiguration().RetryConfiguration
	set := false
	if v := getenv("VAULT_MAX_RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return rc, false, fmt.Errorf("vault: invalid VAULT_MAX_RETRIES %q: %w", v, err)
		}
		rc.RetryMax = n
		set = true
	}
	if v := getenv("VAULT_RETRY_WAIT_MIN"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return rc, false, fmt.Errorf("vault: invalid VAULT_RETRY_WAIT_MIN %q: %w", v, err)
		}
		rc.RetryWaitMin = d
		set = true
	}
	if v := getenv("VAULT_RETRY_WAIT_MAX"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return rc, false, fmt.Errorf("vault: invalid VAULT_RETRY_WAIT_MAX %q: %w", v, err)
		}
		rc.RetryWaitMax = d
		set = true
	}
	return rc, set, nil
}

// joinPath приклеивает необязательный префикс mount к пути секрета.
func joinPath(mount, path string) string {
	if mount == "" {
		return path
	}
	if path == "" {
		return mount
	}
	return mount + "/" + path
}

// collect рекурсивно обходит rv и собирает все поля, реализующие
// secret.Resolvable. Значения из map неадресуемы, поэтому обрабатываются через
// копию с отложенной записью обратно (writebacks выполняются после резолвинга).
func collect(rv reflect.Value) (found []secret.Resolvable, writebacks []func()) {
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		// Тип-секрет — лист: собираем и не углубляемся в его поля.
		if v.CanAddr() {
			if r, ok := v.Addr().Interface().(secret.Resolvable); ok {
				found = append(found, r)
				return
			}
		}
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			t := v.Type()
			for i := 0; i < v.NumField(); i++ {
				if t.Field(i).PkgPath != "" { // неэкспортируемое поле
					continue
				}
				walk(v.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				elem := v.MapIndex(k)
				cp := reflect.New(elem.Type()).Elem()
				cp.Set(elem)
				walk(cp)
				// Значение map неадресуемо: запишем изменённую копию после
				// того, как резолвинг заполнит секреты внутри неё.
				mapVal, key := v, k
				writebacks = append(writebacks, func() {
					mapVal.SetMapIndex(key, cp)
				})
			}
		}
	}
	walk(rv)
	return found, writebacks
}
