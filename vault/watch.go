package vault

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/dvislobokov/sconf"
	"github.com/dvislobokov/sconf/secret"
)

// Watcher управляет фоновым обновлением секретов. Остановите его через Stop
// (или отмену переданного в Watch контекста), когда конфигурация больше не нужна.
type Watcher struct {
	cancel context.CancelFunc
	done   chan struct{}
	n      int
}

// Count возвращает число секретов, поставленных на фоновое обновление.
func (w *Watcher) Count() int {
	if w == nil {
		return 0
	}
	return w.n
}

// Stop останавливает фоновое обновление и дожидается завершения горутин.
// Безопасно вызывать на nil и повторно.
func (w *Watcher) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	<-w.done
}

// watchOptions — настройки Watch.
type watchOptions struct {
	onError func(error)
	backoff time.Duration
}

// WatchOption настраивает поведение Watch.
type WatchOption func(*watchOptions)

// WithErrorHandler задаёт обработчик ошибок фонового обновления. По умолчанию
// ошибки молча игнорируются (прежнее значение секрета сохраняется до следующей
// попытки).
func WithErrorHandler(fn func(error)) WatchOption {
	return func(o *watchOptions) { o.onError = fn }
}

// WithRetryBackoff задаёт паузу перед повторной попыткой после ошибки обновления
// (по умолчанию 30s).
func WithRetryBackoff(d time.Duration) WatchOption {
	return func(o *watchOptions) { o.backoff = d }
}

// Watch запускает фоновое обновление секретов уже загруженной конфигурации
// target (указатель на структуру, обычно результат sconf.Load). Для каждого
// секрета с ненулевым интервалом обновления запускается горутина, обновляющая
// его значение до отмены ctx или вызова Stop. Секреты без интервала (или типы,
// не поддерживающие обновление) пропускаются.
//
// Обычно проще использовать vault.Load, который загружает конфигурацию и сразу
// запускает Watch.
func Watch(ctx context.Context, target any, opts ...WatchOption) (*Watcher, error) {
	o := watchOptions{backoff: 30 * time.Second}
	for _, op := range opts {
		op(&o)
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return &Watcher{done: closedChan()}, nil
	}
	found, _ := collect(rv.Elem())

	var refreshers []secret.Refreshable
	for _, s := range found {
		if r, ok := s.(secret.Refreshable); ok && r.Refresh() > 0 {
			refreshers = append(refreshers, r)
		}
	}
	if len(refreshers) == 0 {
		return &Watcher{done: closedChan()}, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	w := &Watcher{cancel: cancel, done: make(chan struct{}), n: len(refreshers)}

	go func() {
		defer close(w.done)
		var wg sync.WaitGroup
		for _, r := range refreshers {
			wg.Add(1)
			go func(r secret.Refreshable) {
				defer wg.Done()
				refreshLoop(ctx, r, o)
			}(r)
		}
		wg.Wait()
	}()
	return w, nil
}

// refreshLoop периодически обновляет один секрет до отмены ctx.
func refreshLoop(ctx context.Context, r secret.Refreshable, o watchOptions) {
	for {
		d := r.Refresh()
		if d <= 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}

		src, err := newStore(ctx)
		if err == nil {
			err = resolveOne(ctx, src, r)
		}
		if err != nil {
			if o.onError != nil {
				o.onError(err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(o.backoff):
			}
		}
	}
}

// Load загружает конфигурацию через sconf и сразу запускает фоновое обновление
// секретов. Возвращает конфигурацию и Watcher — вызовите Watcher.Stop при
// завершении работы (или отмените ctx).
//
//	ctx := context.Background()
//	cfg, watcher, err := vault.Load[Config](ctx,
//	    sconf.New().AddYAMLFile("appsettings.yaml"),
//	    os.Args[1:],
//	    vault.WithErrorHandler(func(err error) { log.Println("vault refresh:", err) }),
//	)
//	if err != nil { log.Fatal(err) }
//	defer watcher.Stop()
func Load[T any](ctx context.Context, b *sconf.Builder, args []string, opts ...WatchOption) (*T, *Watcher, error) {
	cfg, err := sconf.LoadContext[T](ctx, b, args)
	if err != nil {
		return nil, nil, err
	}
	w, err := Watch(ctx, cfg, opts...)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, w, nil
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
