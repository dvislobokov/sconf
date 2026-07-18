package sconf

import (
	"encoding/json"
	"strings"
	"testing"
)

type helpSettings struct {
	Host string `description:"listen host" default:"0.0.0.0"`
	Mode string `enum:"dev,prod" default:"dev" description:"run mode"`
	DB   struct {
		Host string `env:"DB_HOST" description:"db host"`
	} `yaml:"db"`
}

func TestHelpFormatArg(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, ""},
		{[]string{"--help", "--format", "env"}, "env"},
		{[]string{"--help", "--format=json"}, "json"},
		{[]string{"--format", "yaml", "--help"}, "yaml"},
	}
	for _, tc := range cases {
		if got := helpFormat(tc.args); got != tc.want {
			t.Fatalf("helpFormat(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestUsageFormatEnv(t *testing.T) {
	out, err := UsageFormat[helpSettings]("env", "APP_")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`# listen host (string, default "0.0.0.0")`,
		"APP_HOST=0.0.0.0",
		`# run mode (string, one of dev|prod, default "dev")`,
		"APP_MODE=dev",
		"# db host (string)",
		"DB_HOST=", // тег env: имя как есть, без префикса
	} {
		if !strings.Contains(out, line+"\n") {
			t.Fatalf("missing %q in:\n%s", line, out)
		}
	}
}

func TestUsageFormatJSON(t *testing.T) {
	out, err := UsageFormat[helpSettings]("json", "APP_")
	if err != nil {
		t.Fatal(err)
	}
	var docs []map[string]any
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if len(docs) != 3 {
		t.Fatalf("entries = %d, want 3", len(docs))
	}
	if docs[0]["key"] != "Host" || docs[0]["env"] != "APP_HOST" || docs[0]["default"] != "0.0.0.0" {
		t.Fatalf("docs[0] = %v", docs[0])
	}
	if docs[2]["env"] != "DB_HOST" {
		t.Fatalf("docs[2] = %v", docs[2])
	}
}

func TestUsageFormatYAMLTOMLTable(t *testing.T) {
	y, err := UsageFormat[helpSettings]("yaml", "")
	if err != nil || !strings.Contains(y, "key: Host") {
		t.Fatalf("yaml: err=%v\n%s", err, y)
	}
	tm, err := UsageFormat[helpSettings]("toml", "")
	if err != nil || !strings.Contains(tm, "[[options]]") {
		t.Fatalf("toml: err=%v\n%s", err, tm)
	}
	tbl, err := UsageFormat[helpSettings]("table", "")
	if err != nil || tbl != Usage[helpSettings]() {
		t.Fatalf("table should equal Usage, err=%v", err)
	}
	if _, err := UsageFormat[helpSettings]("xml", ""); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestHelpOutputListsBuiltinFlags(t *testing.T) {
	// Ответ на --help (таблица) содержит встроенные флаги самой Load...
	out, err := helpOutput[helpSettings]("", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--help, -h, -?") ||
		!strings.Contains(out, "--format table|env|json|yaml|toml") {
		t.Fatalf("help must list built-in flags:\n%s", out)
	}
	// ...а машиночитаемая схема ключей — нет.
	env, err := helpOutput[helpSettings]("env", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env, "--format") {
		t.Fatalf("env schema must not list built-in flags:\n%s", env)
	}
}

func TestLoadHelpExits(t *testing.T) {
	exitCode := -1
	prev := osExit
	osExit = func(code int) { exitCode = code }
	defer func() { osExit = prev }()

	_, err := Load[helpSettings](New().AddInMemory(map[string]string{"host": "x"}),
		[]string{"--help"})
	if exitCode != 0 {
		t.Fatalf("Load с --help должен завершать процесс с кодом 0, got %d", exitCode)
	}
	if err != ErrHelp {
		t.Fatalf("после подменённого osExit ожидается ErrHelp, got %v", err)
	}
}

func TestUsageShowsEnvTag(t *testing.T) {
	if out := Usage[helpSettings](); !strings.Contains(out, "(env DB_HOST)") {
		t.Fatalf("usage missing env tag:\n%s", out)
	}
}

func TestEnvTagOverridesValue(t *testing.T) {
	t.Setenv("DB_HOST", "from-env-tag")

	cfg, err := Load[helpSettings](
		New().AddInMemory(map[string]string{"db:host": "from-file"}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Host != "from-env-tag" {
		t.Fatalf("DB.Host = %q, want from-env-tag", cfg.DB.Host)
	}
}

func TestEnvTagLosesToCommandLine(t *testing.T) {
	t.Setenv("DB_HOST", "from-env-tag")

	cfg, err := Load[helpSettings](New(), []string{"--db:host=from-cli"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Host != "from-cli" {
		t.Fatalf("DB.Host = %q, want from-cli", cfg.DB.Host)
	}
}

func TestDumpEnvUsesEnvTag(t *testing.T) {
	cfg, _ := New().AddInMemory(map[string]string{
		"db:host": "db.local",
		"host":    "0.0.0.0",
	}).Build()
	out, err := Dump[helpSettings](cfg, DumpEnv, WithDumpEnvPrefix("APP_"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "DB_HOST=db.local\n") {
		t.Fatalf("env tag name not used:\n%s", out)
	}
	if !strings.Contains(out, "APP_HOST=0.0.0.0\n") {
		t.Fatalf("prefixed name missing:\n%s", out)
	}
}
