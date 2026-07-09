# sconf

[![CI](https://github.com/dvislobokov/sconf/actions/workflows/ci.yml/badge.svg)](https://github.com/dvislobokov/sconf/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dvislobokov/sconf.svg)](https://pkg.go.dev/github.com/dvislobokov/sconf)

A layered configuration library for Go, modeled after
`Microsoft.Extensions.Configuration` (ASP.NET Core). **No `viper`.**

Every source is reduced to a single flat model — `path → string`, with `:` as
the separator (`servers:0:host`). Layers are merged in order (last one wins, per
key), and any struct binds uniformly from any source — including **arrays of
objects assembled from environment variables**.

```go
type Config struct {
    Host    string        `default:"0.0.0.0"`
    Port    int           `default:"8080"`
    Mode    string        `enum:"dev,prod" default:"dev"`
    Servers []struct{ Host string; Port int }
}

cfg, err := sconf.Load[Config](
    sconf.New().
        AddYAMLFile("appsettings.yaml", sconf.Optional()).
        AddEnvironmentVariables("APP_"),
    os.Args[1:],
)
```

## Features

- **Formats out of the box:** JSON, YAML, TOML, environment variables, command
  line, in-memory.
- **Env → arrays of objects** using the `__` → `:` convention.
- **Per-key override** of a single array element from an env var — something
  viper can't do.
- **Wait for files** to appear on disk (Vault sidecar) and **optional** files.
- Reflection binding; key names come from `json` / `yaml` / `toml` / `name` tags.
- `default` (fallback value) and `enum` (allowed values + validation) tags.
- **Usage auto-generation** from your struct, with built-in `--help` handling.
- One entry point — `Load[T]` — plus `Unmarshaler` / `Validator` hooks and
  typed sentinel errors.

## Install

```sh
go get github.com/dvislobokov/sconf
```

Requires Go 1.24+.

## Packages

| Package | Contents |
|---------|----------|
| `sconf` | `Builder`, `Config`, `Load[T]`, `Usage[T]`, errors, option re-exports |
| `sconf/provider` | `JSONFile`, `YAMLFile`, `TOMLFile`, `Env`, `Args`, `Map` |
| `sconf/bind` | reflection binder (`Unmarshaler`, `Validator`) |
| `sconf/internal/flat` | the flat model and path utilities |

## Table of contents

- [Quick start](#quick-start)
- [Layering and precedence](#layering-and-precedence)
- [Environment variables → arrays of objects](#environment-variables--arrays-of-objects)
- [The entry point: `Load[T]`](#the-entry-point-loadt)
- [Ad-hoc access and sections](#ad-hoc-access-and-sections)
- [Waiting for files (Vault sidecar) and optional files](#waiting-for-files-vault-sidecar-and-optional-files)
- [Field tags](#field-tags)
- [Defaults: the `default` tag](#defaults-the-default-tag)
- [Allowed values: the `enum` tag](#allowed-values-the-enum-tag)
- [Usage generation and `--help`](#usage-generation-and---help)
- [Custom parsing: `Unmarshaler`](#custom-parsing-unmarshaler)
- [Validation: `Validator`](#validation-validator)
- [Error handling](#error-handling)
- [Writing a custom source](#writing-a-custom-source)
- [Full example](#full-example)

## Quick start

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
    // Load merges the layers, handles --help, and binds into *Settings.
    // The command-line arguments are folded in by Load itself, as the top layer.
    s, err := sconf.Load[Settings](
        sconf.New().
            AddYAMLFile("appsettings.yaml").
            AddYAMLFile("appsettings.local.yaml", sconf.Optional()). // local overrides
            AddEnvironmentVariables("MYAPP_"),                       // env beats files
        os.Args[1:],
    )
    switch {
    case errors.Is(err, sconf.ErrHelp):
        os.Exit(0) // usage already printed by Load
    case err != nil:
        log.Fatal(err)
    }
    log.Printf("%+v", *s)
}
```

## Layering and precedence

Providers are applied in the order they are added, and **the last one wins, per
key**. Values are merged key by key, not whole file by whole file:

```go
cfg, _ := sconf.New().
    AddInMemory(map[string]string{
        "database:host": "localhost",
        "database:port": "5432",
    }).
    AddEnvironmentVariables("APP_"). // APP_DATABASE__HOST=prod-db
    Build()

cfg.GetString("database:host")  // "prod-db"  (overridden by env)
cfg.GetInt("database:port", 0)  // 5432       (kept from in-memory)
```

## Environment variables → arrays of objects

A double underscore `__` maps to the level separator (the ASP.NET Core
convention):

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

Because everything collapses into one flat model, an env var can override a
**single element** of an array defined in a file. If the file sets
`servers[0] = {file, 1}`, then `MYAPP_SERVERS__0__HOST=env` replaces only
`host`:

```go
// servers[0] == {Host: "env", Port: 1}
```

## The entry point: `Load[T]`

`Load[T]` is the one way to obtain typed configuration. It merges the layers,
handles `--help`, and returns a `*T`.

```go
s, err := sconf.Load[Settings](
    sconf.New().
        AddYAMLFile("appsettings.yaml").
        AddEnvironmentVariables("APP_"),
    os.Args[1:], // or nil, if you don't want CLI args or --help
)
if err != nil {
    // errors.Is(err, sconf.ErrHelp) — help was requested (usage already printed)
    log.Fatal(err)
}
```

`Load[T](builder, args)`:

1. if `args` contains a help flag (`--help`, `-h`, `-?`, `/?`, …), it prints the
   usage generated from `T` and returns `sconf.ErrHelp`;
2. folds `args` in as the last (highest-priority) command-line layer;
3. builds the configuration and binds it into a fresh `*T`.

Missing data is not an error — binding is best-effort: a field with no value
keeps its zero value (or its `default`, if the tag is set).

## Ad-hoc access and sections

For dynamic access without a struct, use `Build() *Config` and its getters:

```go
cfg, _ := sconf.New().AddYAMLFile("appsettings.yaml").Build()

cfg.GetString("database:host")
cfg.GetInt("database:port", 5432) // with a fallback
cfg.GetBool("database:ssl", false)
cfg.Exists("database")            // true
cfg.GetChildren("database")       // ["host", "port", "ssl"]

db := cfg.Section("database")     // a nested section
db.GetString("host")              // same as cfg.GetString("database:host")
```

## Waiting for files (Vault sidecar) and optional files

When a secret is mounted by a sidecar (e.g. Vault Agent), the file may not exist
yet at startup. `Wait` blocks until the file appears; `Optional` keeps the build
from failing if the file is missing or never shows up before the timeout:

```go
cfg, err := sconf.New().
    AddJSONFile("appsettings.json").                         // required: missing -> error
    AddJSONFile("appsettings.local.json", sconf.Optional()). // missing -> skipped
    AddJSONFile(
        "/vault/secrets/db.json",
        sconf.Wait(30*time.Second),          // wait up to 30s for it to appear
        sconf.PollInterval(200*time.Millisecond),
        sconf.Optional(),                    // never appeared -> don't fail
    ).
    Build()
```

| Option | Effect |
|--------|--------|
| `sconf.Optional()` | don't fail if the file is missing / never appears |
| `sconf.Wait(timeout)` | wait for the file to appear (`0` = wait forever) |
| `sconf.PollInterval(d)` | filesystem poll interval while waiting (default 200ms) |

## Field tags

The key name is taken from the `json`, `yaml`, `toml`, or `name` tag (the first
non-empty one), otherwise from the field name. A `-` tag skips the field.
Matching is case-insensitive.

```go
type Settings struct {
    Addr    string `json:"listen_addr"`
    Level   string `yaml:"log_level"`
    Region  string `name:"region"`
    private string // unexported — skipped
    Secret  string `json:"-"` // explicitly skipped
}
```

Supported types: primitives, `time.Duration` (`"5s"`), `time.Time` (RFC 3339),
`*T`, `[]T`, `map[string]T`, nested and embedded structs. Gaps in array indices
are allowed — elements collapse in order.

The full set of tags:

| Tag | Purpose |
|-----|---------|
| `json` / `yaml` / `toml` / `name` | key name (first non-empty) |
| `-` (in json/yaml/toml/name) | skip the field |
| `default:"…"` | fallback value when no source provides the key |
| `enum:"a,b,c"` | closed set of allowed values (validated + shown in usage) |
| `description:"…"` / `usage:"…"` | description for usage (first non-empty) |

## Defaults: the `default` tag

If no source sets a key, the `default` tag value is used. Any source (file, env,
CLI) overrides it.

```go
type Settings struct {
    Host    string        `default:"0.0.0.0"`
    Port    int           `default:"8080"`
    Timeout time.Duration `default:"15s"`
}

// with empty configuration: {0.0.0.0 8080 15s}
```

## Allowed values: the `enum` tag

The `enum` tag defines a closed set. The value (including one coming from
`default`) is checked at bind time, case-insensitively, and normalized to the
canonical spelling from the list. An invalid value returns an error that
satisfies `errors.Is(err, sconf.ErrEnum)`.

```go
type Settings struct {
    Level string `enum:"debug,info,warn,error" default:"info"`
}

// LEVEL=INFO   -> Level == "info"  (canonicalized)
// LEVEL=trace  -> error: config: "Level" = "trace": config: value not allowed (allowed: debug, info, warn, error)
```

## Usage generation and `--help`

`Load` checks for `--help` itself and prints help generated from the struct's
fields (key path, type, `enum`, `default`, description):

```go
type Settings struct {
    Host string `description:"listen host" default:"0.0.0.0"`
    Port int    `default:"8080" usage:"listen port"`
    Mode string `enum:"dev,prod" default:"dev" description:"run mode"`
}

func main() {
    s, err := sconf.Load[Settings](sconf.New() /* ...providers... */, os.Args[1:])
    if errors.Is(err, sconf.ErrHelp) {
        os.Exit(0) // usage already printed by Load
    }
    // ...
}
```

`go run . --help` prints:

```
Options:
  --Host  string  (default "0.0.0.0")  listen host
  --Port  int  (default "8080")  listen port
  --Mode  string  {dev|prod}  (default "dev")  run mode
```

Keys are shown in command-line form (`--section:key`) — exactly how they are
accepted from arguments. You can also generate help manually:
`sconf.Usage[T]() string`, `sconf.HelpRequested(args) bool`, and the structured
data via `sconf.Describe[T]() []sconf.UsageEntry`.

## Custom parsing: `Unmarshaler`

A type can parse its own string form (checked before reflection):

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

## Validation: `Validator`

If a type implements `Validate() error`, it is called after a successful bind;
the error is wrapped with the key path:

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

## Error handling

```go
s, err := sconf.Load[Settings](builder, os.Args[1:])
switch {
case errors.Is(err, sconf.ErrHelp):
    // help requested (--help); usage already printed
    os.Exit(0)
case errors.Is(err, sconf.ErrBindType):
    // a value could not be converted to the field type
    // e.g.: config: cannot bind "Servers:0:Port" (value "abc") to int
case errors.Is(err, sconf.ErrEnum):
    // a value is not in the enum list
case err != nil:
    log.Fatal(err)
}
```

## Writing a custom source

Any type with a `Load` method is a provider. Return flat pairs whose keys use
`:` (case doesn't matter) and pass it to `Add`:

```go
type Provider interface {
    Load() (map[string]string, error)
}

type consulProvider struct{ /* ... */ }

func (p consulProvider) Load() (map[string]string, error) {
    return map[string]string{
        "database:host":  "consul-db",
        "servers:0:host": "web1",
    }, nil
}

cfg, _ := sconf.New().Add(consulProvider{}).Build()
```

## Full example

A complete, runnable service configuration — deeply nested structs, arrays of
objects, maps, `default` / `enum` / `description`, a custom `Unmarshaler`, a
`Validator`, and a merge of YAML + TOML (secret) + env + CLI — lives in
[`example/`](./example):

```sh
go run ./example            # merge all layers and print the result
go run ./example --help     # usage auto-generated from the Config struct
go run ./example --http:port=1234 --logging:level=debug   # CLI override
```

## Development

```sh
go build ./...
go test ./...
go vet ./...
go doc ./...   # godoc, with runnable Example* functions
```
