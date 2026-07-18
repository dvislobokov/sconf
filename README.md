# sconf

[![CI](https://github.com/dvislobokov/sconf/actions/workflows/ci.yml/badge.svg)](https://github.com/dvislobokov/sconf/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dvislobokov/sconf.svg)](https://pkg.go.dev/github.com/dvislobokov/sconf)
[![Go Report Card](https://goreportcard.com/badge/github.com/dvislobokov/sconf)](https://goreportcard.com/report/github.com/dvislobokov/sconf)

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

- **Formats out of the box:** JSON, YAML, TOML, `.env` files, environment variables, command
  line, in-memory.
- **Env → arrays of objects** using the `__` → `:` convention.
- **Per-key override** of a single array element from an env var — something
  viper can't do.
- **Wait for files** to appear on disk (Vault sidecar) and **optional** files.
- **Secrets from Vault** — declare a field as `secret.UserPass` / `secret.Cert` /
  `secret.KV`, put only the *path* in your config, and `Load` fetches the value
  from Vault, with optional **background refresh** (AD/DB every 30 min, PKI by
  TTL). Optional package — the core stays dependency-free.
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
| `sconf` | `Builder`, `Config`, `Load[T]`, `Usage[T]`, errors, option re-exports, built-in Vault integration |
| `sconf/provider` | `JSONFile`, `YAMLFile`, `TOMLFile`, `DotEnvFile`, `Env`, `Args`, `Map` |
| `sconf/bind` | reflection binder (`Unmarshaler`, `Validator`) |
| `sconf/secret` | secret field types (`UserPass`, `Cert`) — no external deps |
| `sconf/internal/vault` | Vault client internals (`vault-client-go`) — used by the core, not imported directly |
| `sconf/internal/flat` | the flat model and path utilities |

## Table of contents

- [Quick start](#quick-start)
- [Layering and precedence](#layering-and-precedence)
- [Environment variables → arrays of objects](#environment-variables--arrays-of-objects)
- [`.env` files](#env-files)
- [The entry point: `Load[T]`](#the-entry-point-loadt)
- [Ad-hoc access and sections](#ad-hoc-access-and-sections)
- [Dumping the merged configuration](#dumping-the-merged-configuration)
- [Waiting for files (Vault sidecar) and optional files](#waiting-for-files-vault-sidecar-and-optional-files)
- [Field tags](#field-tags)
- [Defaults: the `default` tag](#defaults-the-default-tag)
- [Allowed values: the `enum` tag](#allowed-values-the-enum-tag)
- [Usage generation and `--help`](#usage-generation-and---help)
- [Custom parsing: `Unmarshaler`](#custom-parsing-unmarshaler)
- [Validation: `Validator`](#validation-validator)
- [Secrets from Vault](#secrets-from-vault)
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

## `.env` files

For local development you can read a dotenv file directly — no `direnv`, no
exporting. Lines are treated exactly like environment variables (the prefix is
stripped, `__` becomes `:`), but the process environment is not touched:

```go
cfg, err := sconf.Load[Settings](
    sconf.New().
        AddYAMLFile("appsettings.yaml").
        AddDotEnvFile(".env", "APP_", sconf.Optional()). // skipped when absent (CI, prod)
        AddEnvironmentVariables("APP_"),                 // real env still wins
    os.Args[1:],
)
```

```sh
# .env — the usual dotenv syntax
APP_DATABASE__HOST=localhost
export APP_DATABASE__PORT=5432        # optional export prefix
APP_GREETING="hello\nworld"           # double quotes expand \n \t \" \\
APP_TOKEN='as $is'                    # single quotes are literal
```

File options (`Optional`, `Wait`, `PollInterval`) work the same as for
JSON/YAML/TOML files. Multiline values and variable expansion are not
supported. Note that a `.env` holding `VAULT_ADDR` and friends configures the
*process* environment — sconf reads Vault settings from real env vars, so
those still need `direnv`/`docker compose` or manual export.

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

## Dumping the merged configuration

To see what the layered merge actually produced, print it — as flat keys, as
environment variables, or as JSON/YAML/TOML. Pass your config type to get the
`description`/`usage` tags as comments:

```go
cfg, _ := builder.Build()

out, _ := sconf.Dump[Settings](cfg, sconf.DumpKeys)
fmt.Print(out)
// # db host
// database:host = db.local
// database:port = 5432

out, _ = sconf.Dump[Settings](cfg, sconf.DumpEnv, sconf.WithDumpEnvPrefix("APP_"))
fmt.Print(out)
// # db host
// APP_DATABASE__HOST=db.local
// APP_DATABASE__PORT=5432
```

| Format | Output |
|--------|--------|
| `sconf.DumpKeys` | flat `key = value` lines, `#` description comments |
| `sconf.DumpEnv` | `KEY__SUB=value` lines (compatible with `.env` / `AddEnvironmentVariables`), `#` comments |
| `sconf.DumpJSON` / `DumpYAML` / `DumpTOML` | nested document; data only — JSON has no comments, and the YAML/TOML marshalers don't emit them |

Notes:

- `sconf.DumpValues(cfg, format)` is the same without a type (no descriptions).
- All values print as strings — that's the internal model.
- On a `cfg.Section("database")` only that section's keys are printed.
- Secret *fields* hold Vault paths in the config, not secret values — safe to
  print. But an `AddVaultKV` layer puts real values into the tree: mask them
  with `sconf.WithDumpRedact("api_key", "database")` (redacts the key and
  everything under it as `***`).

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
| `env:"NAME"` | read this field from the exact env var `NAME` (see below) |

The `env` tag binds a field to an explicitly named environment variable — no
prefix, no `__` convention:

```go
type Settings struct {
    DB struct {
        Host string `env:"DB_HOST"`
    }
}
// DB_HOST=prod-db  ->  cfg.DB.Host == "prod-db"
```

It participates in layering between the builder's providers and the command
line: `files/env layers < env tag < CLI args`. The name is also shown in
`--help` (`(env DB_HOST)`) and used by the `env` output of `--help --format`
and `Dump`. Inside slice/map elements the tag is ignored — one variable can't
address a particular element.

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

### `--help --format …`

Next to `--help` the service accepts `--format table|env|json|yaml|toml` — so
anyone can see which variables the service understands, in the form they need:

```sh
go run . --help --format env
```

```sh
# listen host (string, default "0.0.0.0")
APP_HOST=0.0.0.0
# run mode (string, one of dev|prod, default "dev")
APP_MODE=dev
# db host (string)
DB_HOST=
```

- `table` (default) — the human-readable listing above;
- `env` — a ready-to-fill `.env` template: real variable names (the prefix is
  taken from the builder's `AddEnvironmentVariables`, an `env` tag wins as-is),
  defaults as values, descriptions as comments;
- `json` / `yaml` / `toml` — the schema as a list of entries
  (`key`, `env`, `type`, `default`, `enum`, `description`) for tooling and docs.

Programmatic access: `sconf.UsageFormat[T](format, envPrefix)`.

### The same over HTTP

`sconf.UsageHandler[T](envPrefix)` serves the schema as an endpoint — a plain
`http.Handler`, so it plugs into any router. The `format` query parameter
selects the output (same five formats, `table` by default); the response is
always bare text:

```go
mux.Handle("/config/usage", sconf.UsageHandler[Config]("APP_"))       // net/http
r.GET("/config/usage", gin.WrapH(sconf.UsageHandler[Config]("APP_"))) // gin
e.GET("/config/usage", echo.WrapHandler(sconf.UsageHandler[Config]("APP_")))
```

```sh
curl localhost:8080/config/usage?format=env
```

Only the schema is served (keys, types, defaults, enum, descriptions) — no
configuration *values* leave the process. An unknown format returns `400`.

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

## Secrets from Vault

Keep secret **values** out of your config files. Put a **path**, get a value —
`sconf` fetches it from [HashiCorp Vault](https://www.vaultproject.io/) at load
time and, if you want, keeps it fresh in the background.

Declare fields with types from `sconf/secret`, and write the full Vault path in
YAML/JSON/TOML:

```go
package main

import (
    "log"
    "os"

    "github.com/dvislobokov/sconf"
    "github.com/dvislobokov/sconf/secret"
)

type Config struct {
    App struct {
        Name string `yaml:"name"`
        Env  string `yaml:"env" enum:"dev,staging,prod" default:"dev"`
    } `yaml:"app"`

    Database struct {
        Host  string          `yaml:"host" default:"localhost"`
        Port  int             `yaml:"port" default:"5432"`
        Creds secret.UserPass `yaml:"creds"` // ← username/password from Vault
    } `yaml:"database"`

    ActiveDirectory secret.UserPass `yaml:"ad_creds"` // ← rotated by Vault
    TLS             secret.Cert     `yaml:"tls_cert"` // ← issued by pki
    APIKey          secret.Value    `yaml:"api_key"`  // ← one KV field
    FeatureFlags    secret.KV       `yaml:"flags"`    // ← a whole KV secret
}

func main() {
    cfg, err := sconf.Load[Config](
        sconf.New().
            AddYAMLFile("appsettings.yaml").
            AddEnvironmentVariables("APP_"),
        os.Args[1:],
        sconf.WithSecretErrorHandler(func(err error) { log.Println("vault refresh:", err) }),
    )
    if err != nil {
        log.Fatal(err)
    }
    // Secrets are filled and refreshed in the background automatically —
    // nothing to stop or manage.

    db := cfg.Database.Creds
    log.Printf("db: %s:%d user=%s", cfg.Database.Host, cfg.Database.Port, db.Username())
    log.Printf("tls serial: %s", cfg.TLS.SerialNumber())
    log.Printf("region flag: %s", cfg.FeatureFlags.Get("region"))
}
```

```yaml
# appsettings.yaml — full Vault paths only, never secret values
app:
  name: billing-api
  env: prod

database:
  host: db.internal
  port: 5432
  creds: database/static-creds/billing-app     # GET → username, password

ad_creds: ad/static-cred/svc-billing            # GET → username, current_password
tls_cert: pki/issue/web?common_name=billing.example.com&ttl=72h   # PUT (issue)
api_key:  secret/data/billing?field=stripe_key  # one KV v2 field
flags:    secret/data/billing/flags             # all KV v2 fields
```

That's the whole integration: values arrive filled, and `secret.UserPass` /
`secret.Cert` are refreshed on a schedule (see [Keeping secrets
fresh](#keeping-secrets-fresh)).

> Values are read through **methods** (`Username()`, `Password()`,
> `Certificate()`, …), not fields, because the background refresher may replace
> them concurrently. The methods return an atomic snapshot and are safe to call
> from any goroutine.

### Secret types

All live in `sconf/secret` and each exposes `Path()` and `Resolved()`.

| Type | Vault op | Accessors | Typical engines |
|------|----------|-----------|-----------------|
| `secret.UserPass` | read (`GET`) | `Username()`, `Password()` | `database` (creds & static-creds), `openldap`, `ad`, `userpass` |
| `secret.Cert` | write (`PUT`) | `Certificate()`, `PrivateKey()`, `IssuingCA()`, `CAChain()`, `SerialNumber()` | `pki` (`issue/<role>`) |
| `secret.KV` | read (`GET`) | `Get(key)`, `Values()` | `kv` v1/v2 |
| `secret.Value` | read (`GET`) | `Get()` | `kv` v1/v2 (single field) |

The config value is always the **full Vault path** — mount, `creds` /
`static-creds` / `issue`, role, everything. Nothing is inferred except the
optional `VAULT_MOUNTPATH` prefix. Extra `?key=value` params are passed to Vault
as the request body (for `pki` issue: `common_name`, `alt_names`, `ttl`, …);
`refresh`, `field`, `username_field`, and `password_field` are reserved for the
resolver and never sent.

### `UserPass` field mapping

`Username()` reads the response field `username`. `Password()` reads
`current_password` when present (that's what the `ad` engine returns), otherwise
`password` (`database`, `openldap`). Because `database`/`openldap` never return a
`current_password` field, this order resolves both cases automatically — even
when an `ad` response also carries a `password` field. Override the field names
for non-standard engines:

```yaml
ad_creds: ad/static-cred/svc              # auto: picks current_password
custom:   secret/path?username_field=login&password_field=secret
```

### KV: three ways to consume it

A KV secret holds arbitrary data, so pick the shape you need (for KV v2 the path
includes the `data` segment; the `data`/`metadata` envelope is unwrapped for
you):

```go
type Config struct {
    Everything secret.KV    `yaml:"kv"`      // whole secret
    OneField   secret.Value `yaml:"one"`     // a single field
}
```

```yaml
kv:  secret/data/myapp               # cfg.Everything.Values() / .Get("host")
one: secret/data/myapp?field=token   # cfg.OneField.Get()
```

`secret.Value` without `?field=` also works when the secret has exactly one
field; otherwise it errors and lists the available fields.

### KV as a config layer

To merge a KV secret's keys straight into the config tree — so they bind to
ordinary fields like any other source — add a Vault KV **layer**. Nested
objects and lists flatten just like every other provider (`key:subkey`,
`list:index`):

```go
sconf.New().
    AddYAMLFile("appsettings.yaml").
    AddVaultKV("secret/data/myapp").                // into the root
    AddVaultKVAt("secret/data/db", "database")      // into a section
```

It's a normal layer: merged in order (later wins) and read from Vault at `Build`
time using the same environment configuration below.

### Keeping secrets fresh

Dynamic and static credentials expire; certificates have a TTL. `sconf.Load`
starts a background refresher for every secret with a refresh interval and
atomically swaps in new values:

| Secret | Default refresh |
|--------|-----------------|
| `secret.UserPass`, `secret.KV`, `secret.Value` | every **30 min** — or sooner if the lease/TTL is shorter, so credentials never expire before renewal |
| `secret.Cert` | by **TTL** — re-issued at ~70% of the certificate's lifetime |

Override per secret with a `?refresh=` param:

```yaml
db_creds: database/creds/app?refresh=10m   # force a 10-minute cadence
```

Lifecycle: the refresher runs entirely inside `sconf` — nothing is returned to
manage. With `Load` it lives for the lifetime of the process; with `LoadContext`
it stops when the context is cancelled:

```go
cfg, err := sconf.LoadContext[Config](ctx, builder, args,
    sconf.WithSecretErrorHandler(func(err error) { log.Println("refresh:", err) }),
    sconf.WithSecretRetryBackoff(15*time.Second),
)
// cancel ctx on shutdown — the background refresh stops with it
```

On a refresh error the previous value is kept and the secret is retried after
the backoff; `WithSecretErrorHandler` lets you observe those errors (by default
they're silent).

### Waiting for Vault at startup

Behind a sidecar proxy (istio, linkerd) the first seconds of a pod's life often
have no egress: requests to Vault fail with connection errors or a `503` from
the proxy. By default `Load` returns that error immediately. Enable a startup
wait and sconf will retry *transient* errors (network failures, `429`/`502`/
`503`/`504`) until the budget runs out:

```go
cfg, err := sconf.Load[Config](builder, os.Args[1:],
    sconf.WithVaultWait(30*time.Second),                  // total wait budget
    sconf.WithVaultWaitInterval(2*time.Second),           // pause between attempts (default 2s)
)
```

Or via the environment — it takes precedence over the options and also applies
to `AddVaultKV` layers:

```sh
VAULT_WAIT=30s
VAULT_WAIT_INTERVAL=2s
```

Non-transient errors (bad credentials, `403`, missing path) are returned
immediately — waiting would not fix them.

**Istio example.** Until the `istio-proxy` sidecar is ready, all egress from
the app container is black-holed or answered with `503 UF` by envoy — so an
app that reads Vault secrets on startup crash-loops. Give it a wait budget in
the Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: billing-api
          env:
            - name: VAULT_ADDR
              value: https://vault.internal:8200
            - name: VAULT_AUTH
              value: kubernetes
            - name: VAULT_K8S_ROLE
              value: billing-api
            - name: VAULT_WAIT          # ride out the sidecar startup window
              value: 30s
            - name: VAULT_WAIT_INTERVAL
              value: 2s
```

This complements istio's own knob — `holdApplicationUntilProxyStarts: true`
(pod annotation `proxy.istio.io/config: '{ "holdApplicationUntilProxyStarts": true }'`)
delays the app container until the sidecar is up, but does not cover an egress
gateway or Vault itself being briefly unavailable; `VAULT_WAIT` handles both.

### Configuration (environment)

Connection settings come from the environment — the same for secret fields and
the `AddVaultKV` layers. Works in Kubernetes and anywhere else.

| Variable | Meaning |
|----------|---------|
| `VAULT_SECRETS_FILE` | local dev: read secrets from this file instead of Vault (see [Local development](#local-development)); when unset, a `vault.secrets` file in the working directory is picked up automatically |
| `VAULT_ADDR` / `VAULT_URL` | server address (**required** when secret fields exist, unless a local secrets file is used) |
| `VAULT_NAMESPACE` | namespace (Vault Enterprise / HCP) |
| `VAULT_MOUNTPATH` | optional prefix prepended to every secret path |
| `VAULT_TIMEOUT` | per-request timeout (default `30s`) |
| `VAULT_WAIT` | total time to wait for Vault to become reachable at startup (default: no waiting) — see [Waiting for Vault at startup](#waiting-for-vault-at-startup) |
| `VAULT_WAIT_INTERVAL` | pause between wait attempts (default `2s`) |
| `VAULT_MAX_RETRIES` | per-request retries of the underlying HTTP client on `5xx`/`412` (default `2`, `-1` disables) |
| `VAULT_RETRY_WAIT_MIN` / `VAULT_RETRY_WAIT_MAX` | pause range between those per-request retries (default `1s`–`1.5s`) |
| `VAULT_SKIP_VERIFY` | skip TLS verification (`1`/`true`) |
| `VAULT_AUTH` | `token` (default), `kubernetes`, or `approle` |
| `VAULT_TOKEN` | token — for `VAULT_AUTH=token` |
| `VAULT_K8S_ROLE` | Kubernetes role (**required** for `VAULT_AUTH=kubernetes`) |
| `VAULT_K8S_MOUNT` | Kubernetes auth mount (default `kubernetes`) |
| `VAULT_K8S_TOKEN_PATH` | service-account JWT path (default `/var/run/secrets/kubernetes.io/serviceaccount/token`) |
| `VAULT_ROLE_ID`, `VAULT_SECRET_ID` | AppRole credentials (**required** for `VAULT_AUTH=approle`) |
| `VAULT_APPROLE_MOUNT` | AppRole auth mount (default `approle`) |

Two `.env` examples:

```sh
# Local dev — token auth
VAULT_ADDR=https://vault.dev.internal:8200
VAULT_TOKEN=hvs.CAESIJ...

# Inside Kubernetes — service-account auth, no static token
VAULT_ADDR=https://vault.internal:8200
VAULT_NAMESPACE=team-billing
VAULT_AUTH=kubernetes
VAULT_K8S_ROLE=billing-api
```

### Local development

You don't need a running Vault to develop locally. Two options:

**1. A local secrets file (no Vault at all).** Set `VAULT_SECRETS_FILE` to a file
mapping each secret path to its fields — the resolver reads from it and never
contacts Vault (no `VAULT_ADDR`, no auth). Your application code and config are
identical to production; only the environment differs.

```sh
VAULT_SECRETS_FILE=./secrets.dev.yaml   # that's the only variable you need
```

```yaml
# secrets.dev.yaml — gitignored. Keys are the SAME full paths as in your config.
database/static-creds/billing-app:
  username: devuser
  password: devpass

pki/issue/web:
  certificate: "-----BEGIN CERTIFICATE-----\ndev\n-----END CERTIFICATE-----"
  private_key: "-----BEGIN PRIVATE KEY-----\ndev\n-----END PRIVATE KEY-----"
  serial_number: dev-01

secret/data/billing:          # KV: put the fields directly
  stripe_key: sk_test_local
  region: eu-central-1
```

The file is re-read on each refresh, so editing it updates values live. It takes
precedence over `VAULT_ADDR` if both are set — handy for overriding a single
environment. `AddVaultKV` layers read from it too. (JSON works as well as YAML.)

If `VAULT_SECRETS_FILE` is not set but a file named `vault.secrets` exists in
the working directory, it is picked up automatically — drop one next to the
binary (and into `.gitignore`) and run with no Vault environment at all. The
explicit `VAULT_SECRETS_FILE` always wins over `vault.secrets`.

**2. A dev-mode Vault.** Run `vault server -dev`, point at it, and seed the
secrets — no code changes, exercises the real client and auth:

```sh
vault server -dev -dev-root-token-id=dev-root &
export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=dev-root
vault kv put secret/billing stripe_key=sk_test_local region=eu-central-1
```

### Failure behavior

- Config **has** secret fields but neither Vault nor a local file is configured
  (no `VAULT_ADDR`/`VAULT_SECRETS_FILE`, or the chosen auth method is missing its
  variables) → `Load` fails with an error wrapping `sconf.ErrVaultNotConfigured`.
  Secrets are never silently left empty.
- Config has **no** secret fields → Vault is never contacted; none of the
  variables are required.

### How the pieces fit

Vault support is built into the core: `sconf.Load` binds the config, fills every
secret field via `sconf/internal/vault`, and starts the background refresh —
no extra imports, no registration, nothing to wire up. If your config has no
secret fields, Vault is never contacted. Adding your own secret type is just
implementing `secret.Resolvable` — the resolver picks it up automatically.

A self-contained runnable demo (with an in-process fake Vault) lives in
[`example/vault`](example/vault): `go run ./example/vault`.

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
