// Command example демонстрирует sconf на реалистичной конфигурации сервиса:
// глубокая вложенность, массивы объектов, map, теги default/enum/description,
// кастомный Unmarshaler, Validator и мерж слоёв YAML + TOML(секрет) + env + CLI.
//
// Запуск:
//
//	go run ./example
//	go run ./example --help
//	go run ./example --http:port=1234 --logging:level=debug
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dvislobokov/sconf"
)

// ---------------------------------------------------------------------------
// Модель конфигурации
// ---------------------------------------------------------------------------

// Config — корневая конфигурация сервиса.
type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Logging  LoggingConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Features map[string]bool // произвольные фича-флаги
	Services []ServiceConfig // массив объектов
}

type AppConfig struct {
	Name    string `default:"app" description:"service name"`
	Env     string `enum:"development,staging,production" default:"development" description:"deployment environment"`
	Version string `default:"0.0.0" description:"build version"`
}

type HTTPConfig struct {
	Host     string   `default:"0.0.0.0" description:"listen host"`
	Port     int      `default:"8080" description:"listen port"`
	CORS     CORS     `description:"cross-origin settings"`
	Timeouts Timeouts `description:"server timeouts"`
}

type CORS struct {
	Origins CSV `description:"allowed origins (comma-separated)"`
}

type Timeouts struct {
	Read  time.Duration `default:"5s" description:"read timeout"`
	Write time.Duration `default:"10s" description:"write timeout"`
	Idle  time.Duration `default:"60s" description:"idle timeout"`
}

type LoggingConfig struct {
	Level  string `enum:"debug,info,warn,error" default:"info" description:"log verbosity"`
	Format string `enum:"json,text" default:"json" description:"log output format"`
}

type DatabaseConfig struct {
	Driver   string    `enum:"postgres,mysql,sqlite" default:"postgres" description:"database driver"`
	Host     string    `default:"localhost" description:"primary host"`
	Port     int       `default:"5432" description:"primary port"`
	Name     string    `description:"database name"`
	User     string    `description:"database user"`
	Password string    `description:"database password (from secret store)"`
	Pool     Pool      `description:"connection pool"`
	Replicas []Replica `description:"read replicas"`
}

type Pool struct {
	MaxOpen  int           `default:"10" description:"max open connections"`
	MaxIdle  int           `default:"2" description:"max idle connections"`
	Lifetime time.Duration `default:"30m" description:"connection max lifetime"`
}

type Replica struct {
	Host string `description:"replica host"`
	Port int    `default:"5432" description:"replica port"`
}

type RedisConfig struct {
	Addr     string `default:"localhost:6379" description:"redis address"`
	DB       int    `default:"0" description:"redis database index"`
	Password string `description:"redis password (from secret store)"`
}

type ServiceConfig struct {
	Name    string        `description:"upstream name"`
	URL     string        `description:"upstream base url"`
	Retries int           `default:"3" description:"retry attempts"`
	Backoff time.Duration `default:"100ms" description:"retry backoff"`
}

// Validate вызывается биндером после заполнения структуры.
func (d *DatabaseConfig) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("database name is required")
	}
	if d.Password == "" {
		return fmt.Errorf("database password is required (secret not mounted?)")
	}
	return nil
}

// CSV — тип с собственным разбором строки конфигурации.
type CSV []string

func (c *CSV) UnmarshalConfig(value string) error {
	var out CSV
	for _, p := range strings.Split(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	*c = out
	return nil
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	dir := sourceDir()

	// Демонстрация переменных среды: обычно они приходят из окружения, здесь
	// выставляем их программно, чтобы пример был самодостаточным.
	//   - перекрытие скаляра
	//   - перекрытие одного поля элемента массива (реплики)
	//   - добавление фича-флага
	os.Setenv("APP_HTTP__PORT", "9090")
	os.Setenv("APP_DATABASE__REPLICAS__0__HOST", "db-replica-1b.internal")
	os.Setenv("APP_FEATURES__BETA_SEARCH", "true")

	// Одна точка входа: собирает слои, проверяет --help и биндит в *Config.
	// Аргументы командной строки Load подмешивает сам, последним слоем.
	cfg, err := sconf.Load[Config](
		sconf.New().
			// 1. База.
			AddYAMLFile(filepath.Join(dir, "appsettings.yaml")).
			// 2. Локальные оверрайды разработчика (может отсутствовать).
			AddYAMLFile(filepath.Join(dir, "appsettings.local.yaml"), sconf.Optional()).
			// 3. Секреты от sidecar. Optional + Wait: не падаем, если файла нет,
			//    но подождём его появления (тут файл уже есть — вернётся сразу).
			AddTOMLFile(filepath.Join(dir, "secrets.toml"),
				sconf.Optional(), sconf.Wait(5*time.Second)).
			// 4. Переменные среды перекрывают файлы.
			AddEnvironmentVariables("APP_"),
		os.Args[1:],
	)
	switch {
	case errors.Is(err, sconf.ErrHelp):
		os.Exit(0) // usage уже напечатан внутри Load
	case err != nil:
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	printConfig(*cfg)
}

// ---------------------------------------------------------------------------
// Вывод
// ---------------------------------------------------------------------------

func printConfig(c Config) {
	fmt.Println("Итоговая конфигурация (после мержа всех слоёв):")
	fmt.Printf("app:      name=%s env=%s version=%s\n", c.App.Name, c.App.Env, c.App.Version)
	fmt.Printf("http:     %s:%d  (порт перекрыт из env)\n", c.HTTP.Host, c.HTTP.Port)
	fmt.Printf("          cors.origins=%v\n", []string(c.HTTP.CORS.Origins))
	fmt.Printf("          timeouts: read=%s write=%s idle=%s\n",
		c.HTTP.Timeouts.Read, c.HTTP.Timeouts.Write, c.HTTP.Timeouts.Idle)
	fmt.Printf("logging:  level=%s format=%s\n", c.Logging.Level, c.Logging.Format)

	fmt.Printf("database: %s://%s@%s:%d/%s  password=%s\n",
		c.Database.Driver, c.Database.User, c.Database.Host, c.Database.Port,
		c.Database.Name, redact(c.Database.Password))
	fmt.Printf("          pool: maxOpen=%d maxIdle=%d lifetime=%s\n",
		c.Database.Pool.MaxOpen, c.Database.Pool.MaxIdle, c.Database.Pool.Lifetime)
	for i, r := range c.Database.Replicas {
		note := ""
		if i == 0 {
			note = "  (host перекрыт из env)"
		}
		fmt.Printf("          replica[%d]: %s:%d%s\n", i, r.Host, r.Port, note)
	}

	fmt.Printf("redis:    addr=%s db=%d password=%s\n",
		c.Redis.Addr, c.Redis.DB, redact(c.Redis.Password))

	fmt.Printf("features: %v  (beta_search добавлен из env)\n", c.Features)

	fmt.Println("services:")
	for _, s := range c.Services {
		fmt.Printf("          - %-10s %s  retries=%d backoff=%s\n",
			s.Name, s.URL, s.Retries, s.Backoff)
	}
}

func redact(s string) string {
	if s == "" {
		return "(empty)"
	}
	return "***"
}

// sourceDir возвращает каталог этого файла, чтобы пример работал независимо
// от текущего рабочего каталога.
func sourceDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(file)
}
