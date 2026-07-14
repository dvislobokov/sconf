package vault

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	vault "github.com/hashicorp/vault-client-go"
)

// Ожидание готовности Vault при старте. Приложение может подниматься за
// sidecar-прокси (например, istio): пока egress не ожил, походы в Vault
// падают сетевой ошибкой или 503 от прокси. waitReady повторяет попытку,
// пока не истечёт суммарный лимит ожидания.

// defaultWaitInterval — пауза между попытками по умолчанию.
const defaultWaitInterval = 2 * time.Second

// waitOptions — настройки ожидания готовности Vault.
type waitOptions struct {
	timeout  time.Duration // суммарное время ожидания; 0 — ожидание выключено
	interval time.Duration // пауза между попытками
}

// loadWait накладывает на программные настройки переменные среды VAULT_WAIT
// (суммарное время ожидания) и VAULT_WAIT_INTERVAL (пауза между попытками) —
// окружение имеет приоритет.
func loadWait(o waitOptions) (waitOptions, error) {
	if o.interval <= 0 {
		o.interval = defaultWaitInterval
	}
	if v := getenv("VAULT_WAIT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return o, fmt.Errorf("vault: invalid VAULT_WAIT %q: %w", v, err)
		}
		o.timeout = d
	}
	if v := getenv("VAULT_WAIT_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return o, fmt.Errorf("vault: invalid VAULT_WAIT_INTERVAL %q: %w", v, err)
		}
		o.interval = d
	}
	return o, nil
}

// waitReady выполняет fn, повторяя её при временных ошибках (см. temporary),
// пока не истечёт o.timeout. При o.timeout == 0 fn выполняется ровно один раз.
func waitReady(ctx context.Context, o waitOptions, fn func() error) error {
	err := fn()
	if err == nil || o.timeout <= 0 || !temporary(err) {
		return err
	}
	deadline := time.Now().Add(o.timeout)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return fmt.Errorf("vault: still unavailable after waiting %s: %w", o.timeout, err)
		}
		pause := o.interval
		if pause > remain {
			pause = remain
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(pause):
		}
		if err = fn(); err == nil || !temporary(err) {
			return err
		}
	}
}

// temporary сообщает, имеет ли смысл повторять попытку: временными считаются
// сетевые ошибки (нет соединения, DNS, таймаут) и ответы 429/502/503/504
// (незапустившийся прокси, sealed/standby Vault). Ошибки конфигурации, доступа
// (403/404) и отменённый контекст временными не являются.
func temporary(err error) bool {
	if err == nil || errors.Is(err, ErrNotConfigured) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var re *vault.ResponseError
	if errors.As(err, &re) {
		switch re.StatusCode {
		case http.StatusTooManyRequests,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}
