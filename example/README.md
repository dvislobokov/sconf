# Пример: конфигурация сервиса

Полноценный пример использования `sconf` на реалистичной конфигурации
микросервиса.

## Запуск

```bash
go run ./example                 # мерж всех слоёв + вывод результата
go run ./example --help          # автогенерация usage из структуры Config
go run ./example --http:port=1234 --logging:level=debug   # оверрайд из CLI
```

## Что демонстрируется

| Возможность | Где в примере |
|-------------|---------------|
| Глубокая вложенность структур | `Config` → `HTTP.Timeouts`, `Database.Pool` |
| Массивы объектов | `Database.Replicas`, `Services` |
| `map[string]T` | `Features` |
| Мерж слоёв (last-wins) | YAML → TOML(секрет) → env → CLI |
| Перекрытие элемента массива из env | `APP_DATABASE__REPLICAS__0__HOST` |
| Секрет от sidecar | `secrets.toml` + `Optional()` + `Wait()` |
| Тег `default` | таймауты, пул, порт, ретраи |
| Тег `enum` + валидация | `App.Env`, `Logging.Level/Format`, `Database.Driver` |
| Тег `description` | текст в `--help` |
| Кастомный `Unmarshaler` | `CORS.Origins` (тип `CSV`) |
| `Validator` | `DatabaseConfig.Validate` (требует name/password) |
| Единая точка входа | `sconf.Load[Config](builder, os.Args[1:])` |
| `--help` (внутри `Load`) | возвращает `sconf.ErrHelp`, usage печатается автоматически |

## Слои конфигурации (по возрастанию приоритета)

1. `appsettings.yaml` — база.
2. `appsettings.local.yaml` — локальные оверрайды разработчика (опционально).
3. `secrets.toml` — секреты от sidecar (опционально, с ожиданием появления).
4. Переменные среды `APP_*` — перекрывают файлы (выставляются в `main` для наглядности).
5. Аргументы командной строки — перекрывают всё.

## Проверить ошибки

```bash
go run ./example --logging:level=trace   # невалидный enum
# config bind error: config: "Logging:Level" = "trace": config: value not allowed (allowed: debug, info, warn, error)
```
