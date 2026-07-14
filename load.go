package sconf

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dvislobokov/sconf/bind"
)

// ErrHelp возвращается Load, если в аргументах запрошена справка. К этому
// моменту usage уже напечатан в stdout — вызывающему остаётся завершить
// программу. Мирроринг поведения flag.ErrHelp из стандартной библиотеки.
var ErrHelp = errors.New("config: help requested")

// Load — основная точка входа. Она:
//
//  1. если в args есть флаг справки (--help и т.п.) — печатает usage,
//     сгенерированный из T, и возвращает ErrHelp; рядом с --help можно
//     передать --format table|env|json|yaml|toml (см. UsageFormat);
//  2. добавляет args последним (высшим по приоритету) слоем командной строки;
//  3. собирает конфигурацию из builder и биндит её в новое значение *T.
//
// Обычно args — это os.Args[1:]. Передайте nil, чтобы не подключать
// командную строку и не проверять справку.
//
//	cfg, err := sconf.Load[Config](
//	    sconf.New().
//	        AddYAMLFile("appsettings.yaml").
//	        AddEnvironmentVariables("APP_"),
//	    os.Args[1:],
//	)
//	switch {
//	case errors.Is(err, sconf.ErrHelp):
//	    os.Exit(0)
//	case err != nil:
//	    log.Fatal(err)
//	}
//
// Если в T есть поля-секреты (см. пакет sconf/secret), после бинда они
// заполняются из Vault, а их фоновое обновление запускается автоматически
// (см. LoadContext). При наличии полей-секретов, но не настроенном окружении
// Vault, Load возвращает ошибку, оборачивающую ErrVaultNotConfigured.
func Load[T any](b *Builder, args []string, opts ...LoadOption) (*T, error) {
	return LoadContext[T](context.Background(), b, args, opts...)
}

// LoadContext идентична Load, но принимает context.Context. Он ограничивает и
// сам поход в Vault при загрузке, и время жизни фонового обновления секретов:
// горутины обновления останавливаются при отмене ctx. У Load контекст фоновый,
// поэтому секреты обновляются до конца жизни процесса — останавливать нечего
// и не нужно. Ошибки фонового обновления по умолчанию игнорируются (секрет
// сохраняет прежнее значение до следующей попытки) — задайте обработчик через
// WithSecretErrorHandler.
func LoadContext[T any](ctx context.Context, b *Builder, args []string, opts ...LoadOption) (*T, error) {
	var o loadOptions
	for _, op := range opts {
		op(&o)
	}

	if HelpRequested(args) {
		out, err := UsageFormat[T](helpFormat(args), builderEnvPrefix(b))
		if err != nil {
			return nil, err
		}
		fmt.Fprint(os.Stdout, out)
		return nil, ErrHelp
	}
	// Поля с тегом env читаются из явно названных переменных среды. Этот слой
	// сильнее провайдеров билдера, но слабее командной строки.
	if tags := envTagValues[T](); len(tags) > 0 {
		b.AddInMemory(tags)
	}
	if len(args) > 0 {
		b.AddCommandLine(args)
	}

	cfg, err := b.Build()
	if err != nil {
		return nil, err
	}

	out := new(T)
	if err := bind.Bind(cfg.m, "", out); err != nil {
		return nil, err
	}
	if err := resolveSecrets(ctx, out, o); err != nil {
		return nil, err
	}
	if err := watchSecrets(ctx, out, o); err != nil {
		return nil, err
	}
	return out, nil
}
