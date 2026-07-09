package sconf_test

import (
	"fmt"
	"strings"

	"github.com/dvislobokov/sconf"
	"github.com/dvislobokov/sconf/provider"
)

// Базовое чтение и мерж слоёв: значения in-memory перекрываются переменными
// среды (последний источник выигрывает per key).
func ExampleBuilder() {
	env := []string{"APP_DATABASE__HOST=prod-db"}

	cfg, err := sconf.New().
		AddInMemory(map[string]string{
			"database:host": "localhost",
			"database:port": "5432",
		}).
		Add(provider.Env("APP_").WithEnviron(func() []string { return env })).
		Build()
	if err != nil {
		panic(err)
	}

	fmt.Println(cfg.GetString("database:host")) // перекрыто из env
	fmt.Println(cfg.GetInt("database:port", 0)) // из in-memory
	// Output:
	// prod-db
	// 5432
}

// Load — основная точка входа: собирает слои и биндит в структуру.
func ExampleLoad() {
	cfg, err := sconf.Load[struct {
		Database struct {
			Host string
			Port int
			SSL  bool
		}
	}](
		sconf.New().AddInMemory(map[string]string{
			"database:host": "db",
			"database:port": "6432",
			"database:ssl":  "true",
		}),
		nil, // без аргументов командной строки
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s:%d ssl=%v\n", cfg.Database.Host, cfg.Database.Port, cfg.Database.SSL)
	// Output:
	// db:6432 ssl=true
}

// Маппинг переменных среды в массив объектов: "__" — разделитель уровней.
func ExampleLoad_envArray() {
	env := []string{
		"APP_SERVERS__0__HOST=a", "APP_SERVERS__0__PORT=10",
		"APP_SERVERS__1__HOST=b", "APP_SERVERS__1__PORT=20",
	}

	cfg, err := sconf.Load[struct {
		Servers []struct {
			Host string
			Port int
		}
	}](
		sconf.New().Add(provider.Env("APP_").WithEnviron(func() []string { return env })),
		nil,
	)
	if err != nil {
		panic(err)
	}
	for _, s := range cfg.Servers {
		fmt.Printf("%s:%d\n", s.Host, s.Port)
	}
	// Output:
	// a:10
	// b:20
}

// Имена ключей берутся из тегов json/yaml/toml/name (первый непустой).
func ExampleLoad_tags() {
	cfg, err := sconf.Load[struct {
		Addr  string `json:"listen_addr"`
		Level string `yaml:"log_level"`
	}](
		sconf.New().AddInMemory(map[string]string{
			"listen_addr": ":8080",
			"log_level":   "debug",
		}),
		nil,
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Addr, cfg.Level)
	// Output:
	// :8080 debug
}

// Автогенерация usage из структуры: тип, default, enum и описание попадают
// в справку. Значения enum также валидируются при биндинге.
func ExampleUsage() {
	type Settings struct {
		Host string `description:"listen host" default:"0.0.0.0"`
		Port int    `default:"8080" usage:"listen port"`
		Mode string `enum:"dev,prod" default:"dev" description:"run mode"`
	}
	fmt.Print(sconf.Usage[Settings]())
	// Output:
	// Options:
	//   --Host  string  (default "0.0.0.0")  listen host
	//   --Port  int  (default "8080")  listen port
	//   --Mode  string  {dev|prod}  (default "dev")  run mode
}

// CSV — тип с собственным разбором через Unmarshaler.
type CSV []string

func (c *CSV) UnmarshalConfig(value string) error {
	*c = strings.Split(value, ",")
	return nil
}

func ExampleUnmarshaler() {
	cfg, err := sconf.Load[struct{ Origins CSV }](
		sconf.New().AddInMemory(map[string]string{
			"origins": "a.com,b.com,c.com",
		}),
		nil,
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(cfg.Origins), cfg.Origins[1])
	// Output:
	// 3 b.com
}
