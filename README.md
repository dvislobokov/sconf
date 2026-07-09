# sconf

[![CI](https://github.com/dvislobokov/sconf/actions/workflows/ci.yml/badge.svg)](https://github.com/dvislobokov/sconf/actions/workflows/ci.yml)

Конфигурационная библиотека для Go в духе `Microsoft.Extensions.Configuration`
(ASP.NET Core). Без `viper`.

Все источники сводятся к плоской модели «путь → строка» с разделителем `:`
(`servers:0:host`). Слои мержатся по порядку (последний выигрывает per key),
после чего любая структура биндится единообразно из любого источника — включая
**массивы объектов из переменных среды**.

## Установка

```sh
go get github.com/dvislobokov/sconf
```

## Возможности

- Из коробки: **JSON**, **YAML**, **TOML**, переменные среды, аргументы CLI, in-memory.
- Переменные среды → массивы объектов (соглашение `__` → `:`).
- Переопределение отдельного элемента массива из env (чего не умеет viper).
- **Ожидание** появления файла на ФС (Vault sidecar) и **опциональные** файлы.
- Биндинг рефлексией; имена ключей — из тегов `json`/`yaml`/`toml`/`name`.
- Теги `default` (значение по умолчанию) и `enum` (допустимые значения + валидация).
- **Автогенерация usage** из структуры (`Usage[T]`) и детект `--help`.
- Дженерик `Get[T]`, секции, sentinel-ошибки, интерфейсы `Unmarshaler`/`Validator`.

## Структура пакетов

```
sconf                Builder, Config, Load[T], Usage[T], ошибки, ре-экспорт опций
sconf/provider       JSONFile, YAMLFile, TOMLFile, Env, Args, Map
sconf/bind           reflection-биндер (Unmarshaler, Validator)
sconf/internal/flat  плоская модель и утилиты путей
```

## Оглавление примеров

- [Быстрый старт](#быстрый-старт)
- [Мерж слоёв и приоритеты](#мерж-слоёв-и-приоритеты)
- [Переменные среды → массивы объектов](#переменные-среды--массивы-объектов)
- [Точка входа: Load\[T\]](#точка-входа-loadt)
- [Точечный доступ и секции](#точечный-доступ-и-секции)
- [Ожидание файла (Vault sidecar) и опциональность](#ожидание-файла-vault-sidecar-и-опциональность)
- [Теги полей](#теги-полей)
- [Значения по умолчанию: default](#значения-по-умолчанию-default)
- [Допустимые значения: enum](#допустимые-значения-enum)
- [Автогенерация usage и --help](#автогенерация-usage-и---help)
- [Кастомный разбор: Unmarshaler](#кастомный-разбор-unmarshaler)
- [Валидация: Validator](#валидация-validator)
- [Обработка ошибок](#обработка-ошибок)
- [Свой источник](#свой-источник)

## Быстрый старт

`appsettings.yaml`:

```yaml
name: my-service
debug: false
timeout: 15s
database:
  host: localhost
  port: 5432
servers:
  - host: web1
    port: 8080
  - host: web2
    port: 8081
```

```go
package main

import (
    "errors"
    "log"
    "os"
    "time"

    "github.com/dvislobokov/sconf"
)

type Settings struct {
    Name     string
    Debug    bool
    Timeout  time.Duration
    Database struct {
        Host string
        Port int
    }
    Servers []struct {
        Host string
        Port int
    }
}

func main() {
    // Load собирает слои, проверяет --help и биндит в *Settings.
    // Аргументы командной строки Load подмешивает сам, последним слоем.
    s, err := sconf.Load[Settings](
        sconf.New().
            AddYAMLFile("appsettings.yaml").
            AddYAMLFile("appsettings.local.yaml", sconf.Optional()). // локальные оверрайды
            AddEnvironmentVariables("MYAPP_"),                       // env перекрывает файлы
        os.Args[1:],
    )
    switch {
    case errors.Is(err, sconf.ErrHelp):
        os.Exit(0) // usage уже напечатан внутри Load
    case err != nil:
        log.Fatal(err)
    }
    log.Printf("%+v", *s)
}
```

`Load[T](builder, args)` — основная точка входа:

1. если в `args` есть флаг справки (`--help` и т.п.) — печатает usage,
   сгенерированный из `T`, и возвращает `sconf.ErrHelp`;
2. подмешивает `args` последним (высшим по приоритету) слоем командной строки;
3. собирает конфигурацию и биндит её в новое значение `*T`.

Передайте `nil` вместо `args`, чтобы не подключать CLI и не проверять справку.

## Мерж слоёв и приоритеты

Провайдеры применяются по порядку добавления, **последний выигрывает per key**.
Значения объединяются по отдельным ключам, а не целыми файлами:

```go
cfg, _ := sconf.New().
    AddInMemory(map[string]string{
        "database:host": "localhost",
        "database:port": "5432",
    }).
    AddEnvironmentVariables("APP_"). // APP_DATABASE__HOST=prod-db
    Build()

cfg.GetString("database:host")  // "prod-db"  (перекрыто из env)
cfg.GetInt("database:port", 0)  // 5432       (осталось из in-memory)
```

## Переменные среды → массивы объектов

Двойное подчёркивание `__` — разделитель уровней (как в ASP.NET Core):

```
MYAPP_SERVERS__0__HOST=a   MYAPP_SERVERS__0__PORT=10
MYAPP_SERVERS__1__HOST=b   MYAPP_SERVERS__1__PORT=20
```

```go
type Settings struct {
    Servers []struct {
        Host string
        Port int
    }
}
s, _ := sconf.Load[Settings](sconf.New().AddEnvironmentVariables("MYAPP_"), nil)
// s.Servers == [{a 10} {b 20}]
```

Поскольку всё сводится к одной плоской модели, env-переменная может перекрыть
**один элемент** массива из файла. Файл задаёт `servers[0] = {file, 1}`, а
`MYAPP_SERVERS__0__HOST=env` заменит только `host`:

```go
// servers[0] == {Host: "env", Port: 1}
```

## Точка входа: Load[T]

`Load[T]` — единственный способ получить типизированную конфигурацию: он
собирает слои, обрабатывает `--help` и возвращает `*T`.

```go
type Settings struct {
    Database struct {
        Host string
        Port int
        SSL  bool
    }
}

s, err := sconf.Load[Settings](
    sconf.New().
        AddYAMLFile("appsettings.yaml").
        AddEnvironmentVariables("APP_"),
    os.Args[1:], // или nil, если CLI и --help не нужны
)
if err != nil {
    // errors.Is(err, sconf.ErrHelp) — была запрошена справка (usage напечатан)
    log.Fatal(err)
}
_ = s.Database.Host
```

При отсутствии данных для поля остаётся нулевое значение (либо `default`,
если задан тег). Ошибок «ключа нет» нет — конфигурация best-effort.

## Точечный доступ и секции

Для динамического доступа без структуры используйте `Build() *Config` и его
геттеры:

```go
cfg, _ := sconf.New().AddYAMLFile("appsettings.yaml").Build()

cfg.GetString("database:host")
cfg.GetInt("database:port", 5432) // с дефолтом
cfg.GetBool("database:ssl", false)
cfg.Exists("database")            // true
cfg.GetChildren("database")       // ["host", "port", "ssl"]

db := cfg.Section("database")     // вложенная секция
db.GetString("host")              // как cfg.GetString("database:host")
```

## Ожидание файла (Vault sidecar) и опциональность

Когда секрет монтируется sidecar-контейнером (например Vault Agent), файла
может ещё не быть на старте. `Wait` блокирует `Build`, пока файл не появится;
`Optional` не даёт упасть, если файла нет или он не появился за таймаут:

```go
cfg, err := sconf.New().
    AddJSONFile("appsettings.json").                    // обязателен: нет файла -> ошибка
    AddJSONFile("appsettings.local.json", sconf.Optional()). // нет файла -> пропускаем
    AddJSONFile(
        "/vault/secrets/db.json",
        sconf.Wait(30*time.Second),          // ждать появления до 30с
        sconf.PollInterval(200*time.Millisecond),
        sconf.Optional(),                    // не появился -> не падаем
    ).
    Build()
```

| Опция | Назначение |
|-------|-----------|
| `sconf.Optional()` | не падать, если файла нет / он не появился |
| `sconf.Wait(timeout)` | ждать появления файла (`0` — без ограничения) |
| `sconf.PollInterval(d)` | интервал опроса ФС при ожидании (по умолчанию 200ms) |

## Теги полей

Имя ключа берётся из тегов `json` → `yaml` → `toml` → `name` (первый непустой),
иначе — имя поля. Тег `-` пропускает поле. Сравнение регистронезависимо.

```go
type Settings struct {
    Addr    string `json:"listen_addr"`
    Level   string `yaml:"log_level"`
    Region  string `name:"region"`
    private string // неэкспортируемое — пропускается
    Secret  string `json:"-"` // явно пропущено
}
```

Поддерживаемые типы: примитивы, `time.Duration` (`"5s"`), `time.Time` (RFC3339),
`*T`, `[]T`, `map[string]T`, вложенные и встроенные (embedded) структуры.
Дыры в индексах массивов допустимы — элементы схлопываются по порядку.

Полный набор тегов:

| Тег | Назначение |
|-----|-----------|
| `json` / `yaml` / `toml` / `name` | имя ключа (первый непустой) |
| `-` (в json/yaml/toml/name) | пропустить поле |
| `default:"..."` | значение по умолчанию, если ключа нет в источниках |
| `enum:"a,b,c"` | список допустимых значений (валидация + вывод в usage) |
| `description:"..."` / `usage:"..."` | описание для usage (первый непустой) |

## Значения по умолчанию: default

Если ни один источник не задал ключ, используется значение из тега `default`.
Любой источник (файл, env, CLI) перекрывает его.

```go
type Settings struct {
    Host    string        `default:"0.0.0.0"`
    Port    int           `default:"8080"`
    Timeout time.Duration `default:"15s"`
}

var s Settings
cfg.Bind("", &s) // при пустой конфигурации: {0.0.0.0 8080 15s}
```

## Допустимые значения: enum

Тег `enum` задаёт закрытый список. Значение (в т.ч. из `default`) проверяется
при биндинге регистронезависимо и приводится к каноничному написанию из списка;
недопустимое значение возвращает ошибку с `errors.Is(err, sconf.ErrEnum)`.

```go
type Settings struct {
    Level string `enum:"debug,info,warn,error" default:"info"`
}

// LEVEL=INFO   -> Level == "info"  (канонизировано)
// LEVEL=trace  -> ошибка: config: "Level" = "trace": config: value not allowed (allowed: debug, info, warn, error)
```

## Автогенерация usage и --help

`Load` проверяет `--help` сам и печатает справку, сгенерированную из полей
структуры (путь-ключ, тип, `enum`, `default`, описание):

```go
type Settings struct {
    Host string `description:"listen host" default:"0.0.0.0"`
    Port int    `default:"8080" usage:"listen port"`
    Mode string `enum:"dev,prod" default:"dev" description:"run mode"`
}

func main() {
    s, err := sconf.Load[Settings](sconf.New() /* ...провайдеры... */, os.Args[1:])
    if errors.Is(err, sconf.ErrHelp) {
        os.Exit(0) // usage уже напечатан внутри Load
    }
    // ...
}
```

`go run . --help` печатает:

```
Options:
  --Host  string  (default "0.0.0.0")  listen host
  --Port  int  (default "8080")  listen port
  --Mode  string  {dev|prod}  (default "dev")  run mode
```

Ключи выводятся в форме командной строки (`--section:key`) — так же они
принимаются из аргументов. Справку можно получить и вручную:
`sconf.Usage[T]() string`, `sconf.HelpRequested(args) bool`, а структурированные
данные — `sconf.Describe[T]() []sconf.UsageEntry`.

## Кастомный разбор: Unmarshaler

Тип может сам разобрать своё строковое представление (проверяется до reflection):

```go
type CSV []string

func (c *CSV) UnmarshalConfig(value string) error {
    *c = strings.Split(value, ",")
    return nil
}

// ORIGINS=a.com,b.com,c.com  ->  Origins == ["a.com","b.com","c.com"]
type Settings struct{ Origins CSV }
s, _ := sconf.Load[Settings](sconf.New().AddEnvironmentVariables(""), nil)
```

## Валидация: Validator

Если тип реализует `Validate() error`, он вызывается после успешного бинда;
ошибка оборачивается путём ключа:

```go
type DB struct {
    Host string
    Port int
}

func (d *DB) Validate() error {
    if d.Port == 0 {
        return errors.New("port is required")
    }
    return nil
}

type Settings struct{ Database DB }
_, err := sconf.Load[Settings](sconf.New() /* ... */, nil)
// err: config: validate "Database": port is required
```

## Обработка ошибок

```go
s, err := sconf.Load[Settings](builder, os.Args[1:])
switch {
case errors.Is(err, sconf.ErrHelp):
    // запрошена справка (--help); usage уже напечатан
    os.Exit(0)
case errors.Is(err, sconf.ErrBindType):
    // значение нельзя привести к типу поля
    // напр.: config: cannot bind "Servers:0:Port" (value "abc") to int
case errors.Is(err, sconf.ErrEnum):
    // значение не входит в список enum
case err != nil:
    log.Fatal(err)
}
```

## Свой источник

Любой тип с методом `Load` — это провайдер. Верните плоские пары с ключами
через `:` (регистр не важен) и передайте в `Add`:

```go
type Provider interface {
    Load() (map[string]string, error)
}

type consulProvider struct{ /* ... */ }

func (p consulProvider) Load() (map[string]string, error) {
    return map[string]string{
        "database:host": "consul-db",
        "servers:0:host": "web1",
    }, nil
}

cfg, _ := sconf.New().Add(consulProvider{}).Build()
```

## Команды

```bash
go build ./...
go test ./...
go vet ./...
go doc ./...   # godoc с примерами Example*
```
```
