package sconf_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dvislobokov/sconf"
	"github.com/dvislobokov/sconf/provider"
)

type Server struct {
	Host string
	Port int
}

type AppSettings struct {
	Name     string
	Debug    bool
	Timeout  time.Duration
	Servers  []Server
	Database struct {
		Host     string
		Password string
	}
	Tags map[string]string
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMergeJSONYAMLTOML(t *testing.T) {
	dir := t.TempDir()
	jsonFile := write(t, dir, "a.json", `{
		"name": "base", "debug": false, "timeout": "5s",
		"servers": [{"host": "j0", "port": 1}],
		"database": {"host": "localhost"}
	}`)
	yamlFile := write(t, dir, "b.yaml", "debug: true\ndatabase:\n  host: yamlhost\n")
	tomlFile := write(t, dir, "c.toml", "name = \"toml-wins\"\n")

	s, err := sconf.Load[AppSettings](
		sconf.New().
			AddJSONFile(jsonFile).
			AddYAMLFile(yamlFile).
			AddTOMLFile(tomlFile),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "toml-wins" || !s.Debug || s.Timeout != 5*time.Second {
		t.Errorf("got %+v", s)
	}
	if s.Database.Host != "yamlhost" {
		t.Errorf("database.host=%q", s.Database.Host)
	}
	if len(s.Servers) != 1 || s.Servers[0].Host != "j0" || s.Servers[0].Port != 1 {
		t.Errorf("servers=%+v", s.Servers)
	}
}

func TestEnvArrayOfObjects(t *testing.T) {
	env := []string{
		"MYAPP_NAME=envapp",
		"MYAPP_SERVERS__0__HOST=a", "MYAPP_SERVERS__0__PORT=10",
		"MYAPP_SERVERS__1__HOST=b", "MYAPP_SERVERS__1__PORT=20",
		"MYAPP_DATABASE__PASSWORD=secret",
		"MYAPP_TAGS__ENV=prod",
		"IGNORED=x",
	}
	s, err := sconf.Load[AppSettings](
		sconf.New().Add(provider.Env("MYAPP_").WithEnviron(func() []string { return env })),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "envapp" {
		t.Errorf("name=%q", s.Name)
	}
	if len(s.Servers) != 2 ||
		s.Servers[0] != (Server{"a", 10}) || s.Servers[1] != (Server{"b", 20}) {
		t.Errorf("servers=%+v", s.Servers)
	}
	if s.Database.Password != "secret" || s.Tags["env"] != "prod" {
		t.Errorf("db/tags: %+v %+v", s.Database, s.Tags)
	}
}

func TestEnvOverridesFileArrayElement(t *testing.T) {
	dir := t.TempDir()
	jsonFile := write(t, dir, "a.json", `{"servers":[{"host":"file","port":1}]}`)
	env := []string{"APP_SERVERS__0__HOST=env"}

	s, err := sconf.Load[AppSettings](
		sconf.New().
			AddJSONFile(jsonFile).
			Add(provider.Env("APP_").WithEnviron(func() []string { return env })),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	// env перекрыл только host, port сохранился из файла.
	if len(s.Servers) != 1 || s.Servers[0].Host != "env" || s.Servers[0].Port != 1 {
		t.Errorf("servers=%+v", s.Servers)
	}
}

func TestOptionalMissingFile(t *testing.T) {
	cfg, err := sconf.New().
		AddJSONFile(filepath.Join(t.TempDir(), "nope.json"), sconf.Optional()).
		AddInMemory(map[string]string{"name": "fallback"}).
		Build()
	if err != nil {
		t.Fatalf("необязательный отсутствующий файл не должен падать: %v", err)
	}
	if cfg.GetString("name") != "fallback" {
		t.Errorf("name=%q", cfg.GetString("name"))
	}
}

func TestRequiredMissingFileFails(t *testing.T) {
	_, err := sconf.New().AddJSONFile(filepath.Join(t.TempDir(), "nope.json")).Build()
	if err == nil {
		t.Fatal("ожидалась ошибка для обязательного отсутствующего файла")
	}
}

func TestWaitForFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "late.json")
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(path, []byte(`{"name":"appeared"}`), 0o644)
	}()

	start := time.Now()
	cfg, err := sconf.New().
		AddJSONFile(path, sconf.Wait(5*time.Second), sconf.PollInterval(50*time.Millisecond)).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Error("Build вернулся слишком быстро — ожидание не сработало")
	}
	if cfg.GetString("name") != "appeared" {
		t.Errorf("name=%q", cfg.GetString("name"))
	}
}

func TestWaitTimeoutOptional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never.json")
	cfg, err := sconf.New().
		AddJSONFile(path, sconf.Wait(200*time.Millisecond), sconf.PollInterval(50*time.Millisecond), sconf.Optional()).
		AddInMemory(map[string]string{"name": "default"}).
		Build()
	if err != nil {
		t.Fatalf("не должно падать: %v", err)
	}
	if cfg.GetString("name") != "default" {
		t.Errorf("name=%q", cfg.GetString("name"))
	}
}

func TestLoadAndSection(t *testing.T) {
	type Root struct {
		Database struct {
			Host string
			Port int
			SSL  bool
		}
	}
	root, err := sconf.Load[Root](
		sconf.New().AddInMemory(map[string]string{
			"database:host": "db", "database:port": "5432", "database:ssl": "true",
		}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if root.Database.Host != "db" || root.Database.Port != 5432 || !root.Database.SSL {
		t.Errorf("Load=%+v", root.Database)
	}

	// Config-геттеры для точечного доступа остаются доступны через Build.
	cfg, err := sconf.New().AddInMemory(map[string]string{
		"database:host": "db", "database:port": "5432", "database:ssl": "true",
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	sec := cfg.Section("database")
	if sec.GetInt("port", 0) != 5432 || !sec.GetBool("ssl", false) {
		t.Errorf("section getters failed")
	}
}

func TestLoadEnumError(t *testing.T) {
	type Settings struct {
		Level string `enum:"debug,info"`
	}
	_, err := sconf.Load[Settings](
		sconf.New().AddInMemory(map[string]string{"level": "trace"}),
		nil,
	)
	if !errors.Is(err, sconf.ErrEnum) {
		t.Fatalf("ожидался ErrEnum, got %v", err)
	}
}

func TestCommandLine(t *testing.T) {
	cfg, err := sconf.New().
		AddCommandLine([]string{"--name=cli", "--database:port", "6000", "--debug=true"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetString("name") != "cli" || cfg.GetInt("database:port", 0) != 6000 || !cfg.GetBool("debug", false) {
		t.Errorf("cli parse failed: name=%q port=%d", cfg.GetString("name"), cfg.GetInt("database:port", 0))
	}
}
