package sconf

import (
	"strings"
	"testing"
)

type dumpSettings struct {
	App struct {
		Name string `description:"application name"`
	}
	Database struct {
		Host string `description:"db host"`
		Port int
	}
	Servers []struct {
		Host string `description:"server host"`
	}
}

func dumpConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := New().AddInMemory(map[string]string{
		"app:name":       "billing",
		"database:host":  "db.local",
		"database:port":  "5432",
		"servers:0:host": "a",
		"servers:1:host": "b",
		"api_key":        "s3cret",
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDumpKeys(t *testing.T) {
	out, err := Dump[dumpSettings](dumpConfig(t), DumpKeys)
	if err != nil {
		t.Fatal(err)
	}
	want := `api_key = s3cret
# application name
app:name = billing
# db host
database:host = db.local
database:port = 5432
# server host
servers:0:host = a
# server host
servers:1:host = b
`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestDumpEnv(t *testing.T) {
	out, err := Dump[dumpSettings](dumpConfig(t), DumpEnv, WithDumpEnvPrefix("APP_"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"APP_APP__NAME=billing",
		"# db host",
		"APP_DATABASE__HOST=db.local",
		"APP_SERVERS__0__HOST=a",
	} {
		if !strings.Contains(out, line+"\n") {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}
}

func TestDumpEnvQuoting(t *testing.T) {
	cfg, _ := New().AddInMemory(map[string]string{
		"msg":   "hello world # not comment",
		"empty": "",
		"plain": "v1",
	}).Build()
	out, err := DumpValues(cfg, DumpEnv)
	if err != nil {
		t.Fatal(err)
	}
	want := `EMPTY=
MSG="hello world # not comment"
PLAIN=v1
`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestDumpJSON(t *testing.T) {
	out, err := DumpValues(dumpConfig(t), DumpJSON)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "api_key": "s3cret",
  "app": {
    "name": "billing"
  },
  "database": {
    "host": "db.local",
    "port": "5432"
  },
  "servers": [
    {
      "host": "a"
    },
    {
      "host": "b"
    }
  ]
}
`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestDumpYAMLAndTOML(t *testing.T) {
	cfg := dumpConfig(t)

	y, err := DumpValues(cfg, DumpYAML)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"database:", "    host: db.local", "- host: a"} {
		if !strings.Contains(y, s) {
			t.Fatalf("yaml missing %q:\n%s", s, y)
		}
	}

	tm, err := DumpValues(cfg, DumpTOML)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"[database]", "host = 'db.local'", "[[servers]]"} {
		if !strings.Contains(tm, s) {
			t.Fatalf("toml missing %q:\n%s", s, tm)
		}
	}
}

func TestDumpRedact(t *testing.T) {
	out, err := DumpValues(dumpConfig(t), DumpKeys, WithDumpRedact("api_key", "database"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "api_key = ***") {
		t.Fatalf("api_key not redacted:\n%s", out)
	}
	if !strings.Contains(out, "database:host = ***") || !strings.Contains(out, "database:port = ***") {
		t.Fatalf("database section not redacted:\n%s", out)
	}
	if !strings.Contains(out, "app:name = billing") {
		t.Fatalf("app:name should stay:\n%s", out)
	}
}

func TestDumpSection(t *testing.T) {
	out, err := DumpValues(dumpConfig(t).Section("database"), DumpKeys)
	if err != nil {
		t.Fatal(err)
	}
	want := "host = db.local\nport = 5432\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestDumpUnknownFormat(t *testing.T) {
	if _, err := DumpValues(dumpConfig(t), DumpFormat("xml")); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestDumpValueSectionConflict(t *testing.T) {
	// Ключ одновременно и значение, и секция — секция побеждает, дамп не падает.
	cfg, _ := New().AddInMemory(map[string]string{
		"a":   "scalar",
		"a:b": "nested",
	}).Build()
	out, err := DumpValues(cfg, DumpJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"b": "nested"`) {
		t.Fatalf("got:\n%s", out)
	}
}
