// Package provider содержит источники конфигурации. Каждый источник
// реализует Load() (map[string]string, error) и возвращает плоские пары
// "путь -> значение" (ключи в исходном регистре, разделитель ":").
package provider

import (
	"fmt"
	"os"
	"time"

	"github.com/dvislobokov/sconf/internal/flat"
)

// FileOption настраивает файловый источник.
type FileOption func(*fileOptions)

type fileOptions struct {
	optional     bool
	wait         bool
	waitTimeout  time.Duration
	pollInterval time.Duration
}

// Optional помечает файл необязательным: отсутствие файла (или его непоявление
// при Wait) не приводит к ошибке.
func Optional() FileOption {
	return func(o *fileOptions) { o.optional = true }
}

// Wait включает ожидание появления файла (напр. секрет от Vault sidecar).
// timeout == 0 — ожидание без ограничения по времени.
func Wait(timeout time.Duration) FileOption {
	return func(o *fileOptions) {
		o.wait = true
		o.waitTimeout = timeout
	}
}

// PollInterval задаёт интервал опроса ФС при ожидании файла (по умолчанию 200ms).
func PollInterval(d time.Duration) FileOption {
	return func(o *fileOptions) { o.pollInterval = d }
}

// parseFunc разбирает содержимое файла в плоские пары.
type parseFunc func(data []byte) (map[string]string, error)

type fileProvider struct {
	path  string
	opts  fileOptions
	parse parseFunc
}

func newFileProvider(path string, parse parseFunc, opts []FileOption) *fileProvider {
	o := fileOptions{pollInterval: 200 * time.Millisecond}
	for _, opt := range opts {
		opt(&o)
	}
	if o.pollInterval <= 0 {
		o.pollInterval = 200 * time.Millisecond
	}
	return &fileProvider{path: path, opts: o, parse: parse}
}

func (f *fileProvider) Load() (map[string]string, error) {
	if f.opts.wait {
		if err := waitForFile(f.path, f.opts.waitTimeout, f.opts.pollInterval); err != nil {
			if f.opts.optional {
				return map[string]string{}, nil
			}
			return nil, err
		}
	}

	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) && f.opts.optional {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("config: read %q: %w", f.path, err)
	}

	m, err := f.parse(data)
	if err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", f.path, err)
	}
	return m, nil
}

func waitForFile(path string, timeout, poll time.Duration) error {
	if fileExists(path) {
		return nil
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for range ticker.C {
		if fileExists(path) {
			return nil
		}
		if timeout > 0 && time.Now().After(deadline) {
			return fmt.Errorf("config: file %q did not appear within %s", path, timeout)
		}
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// flattenTree — общий помощник для форматных парсеров.
func flattenTree(root interface{}) map[string]string {
	out := map[string]string{}
	flat.Flatten(out, "", root)
	return out
}
