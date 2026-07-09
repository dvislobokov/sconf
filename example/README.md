# Example: service configuration

A complete, runnable example of using `sconf` with a realistic microservice
configuration.

## Run

```bash
go run ./example                 # merge all layers and print the result
go run ./example --help          # usage auto-generated from the Config struct
go run ./example --http:port=1234 --logging:level=debug   # CLI override
```

## What it demonstrates

| Feature | Where in the example |
|---------|----------------------|
| Deeply nested structs | `Config` → `HTTP.Timeouts`, `Database.Pool` |
| Arrays of objects | `Database.Replicas`, `Services` |
| `map[string]T` | `Features` |
| Layer merge (last wins) | YAML → TOML (secret) → env → CLI |
| Overriding one array element from env | `APP_DATABASE__REPLICAS__0__HOST` |
| Secret from a sidecar | `secrets.toml` + `Optional()` + `Wait()` |
| The `default` tag | timeouts, pool, port, retries |
| The `enum` tag + validation | `App.Env`, `Logging.Level/Format`, `Database.Driver` |
| The `description` tag | text shown in `--help` |
| Custom `Unmarshaler` | `CORS.Origins` (the `CSV` type) |
| `Validator` | `DatabaseConfig.Validate` (requires name/password) |
| Single entry point | `sconf.Load[Config](builder, os.Args[1:])` |
| `--help` (inside `Load`) | returns `sconf.ErrHelp`; usage is printed automatically |

## Configuration layers (lowest to highest priority)

1. `appsettings.yaml` — the base.
2. `appsettings.local.yaml` — developer's local overrides (optional).
3. `secrets.toml` — secrets from a sidecar (optional, waits for the file).
4. `APP_*` environment variables — override files (set in `main` for clarity).
5. Command-line arguments — override everything.

## Trying the error paths

```bash
go run ./example --logging:level=trace   # invalid enum
# config error: config: "Logging:Level" = "trace": config: value not allowed (allowed: debug, info, warn, error)
```
