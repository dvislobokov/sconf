package sconf

import (
	"context"
	"time"

	"github.com/dvislobokov/sconf/internal/vault"
)

// ErrVaultNotConfigured возвращается (через %w) из Load, когда в конфигурации
// есть поля-секреты, но окружение Vault не настроено (не задан VAULT_ADDR и т.п.).
var ErrVaultNotConfigured = vault.ErrNotConfigured

// loadOptions — настройки Load, касающиеся секретов.
type loadOptions struct {
	watch   []vault.WatchOption
	resolve []vault.ResolveOption
}

// LoadOption настраивает поведение Load.
type LoadOption func(*loadOptions)

// WithSecretErrorHandler задаёт обработчик ошибок фонового обновления секретов.
// По умолчанию ошибки молча игнорируются (прежнее значение секрета сохраняется
// до следующей попытки).
func WithSecretErrorHandler(fn func(error)) LoadOption {
	return func(o *loadOptions) { o.watch = append(o.watch, vault.WithErrorHandler(fn)) }
}

// WithSecretRetryBackoff задаёт паузу перед повторной попыткой после ошибки
// фонового обновления секрета (по умолчанию 30s).
func WithSecretRetryBackoff(d time.Duration) LoadOption {
	return func(o *loadOptions) { o.watch = append(o.watch, vault.WithRetryBackoff(d)) }
}

// WithVaultWait задаёт суммарное время ожидания доступности Vault при
// первичной загрузке секретов. Полезно, когда приложение стартует за
// sidecar-прокси (istio и т.п.) и Vault недоступен первые секунды жизни пода:
// временные ошибки (сеть, 5xx от прокси) повторяются, пока не истечёт timeout.
// По умолчанию ожидание выключено — первая же ошибка возвращается из Load.
// Переменная среды VAULT_WAIT имеет приоритет над опцией.
func WithVaultWait(timeout time.Duration) LoadOption {
	return func(o *loadOptions) { o.resolve = append(o.resolve, vault.WithWait(timeout)) }
}

// WithVaultWaitInterval задаёт паузу между попытками ожидания Vault
// (по умолчанию 2s). Переменная среды VAULT_WAIT_INTERVAL имеет приоритет.
func WithVaultWaitInterval(d time.Duration) LoadOption {
	return func(o *loadOptions) { o.resolve = append(o.resolve, vault.WithWaitInterval(d)) }
}

// resolveSecrets заполняет поля-секреты target из Vault. Если полей-секретов
// нет, ничего не делает и не требует настроенного окружения Vault.
func resolveSecrets(ctx context.Context, target any, o loadOptions) error {
	return vault.Resolve(ctx, target, o.resolve...)
}

// watchSecrets запускает фоновое обновление секретов target. Горутины
// обновления живут до отмены ctx; наружу ничего не возвращается.
func watchSecrets(ctx context.Context, target any, o loadOptions) error {
	_, err := vault.Watch(ctx, target, o.watch...)
	return err
}
