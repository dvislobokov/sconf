package provider

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeDotEnv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDotEnvFile(t *testing.T) {
	path := writeDotEnv(t, `
# комментарий
APP_HOST=localhost
export APP_PORT=8080
APP_DATABASE__HOST=db.local     # хвостовой комментарий
APP_MESSAGE="hello\nworld # not a comment"
APP_TOKEN='as $is \n'
APP_EMPTY=
OTHER_IGNORED=x

APP_SERVERS__0__HOST=a
`)

	got, err := DotEnvFile(path, "APP_").Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[string]string{
		"HOST":           "localhost",
		"PORT":           "8080",
		"DATABASE:HOST":  "db.local",
		"MESSAGE":        "hello\nworld # not a comment",
		"TOKEN":          `as $is \n`,
		"EMPTY":          "",
		"SERVERS:0:HOST": "a",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestDotEnvFileNoPrefix(t *testing.T) {
	path := writeDotEnv(t, "HOST=h\nDB__PORT=5432\n")
	got, err := DotEnvFile(path, "").Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["HOST"] != "h" || got["DB:PORT"] != "5432" {
		t.Fatalf("got %#v", got)
	}
}

func TestDotEnvFileErrors(t *testing.T) {
	cases := []struct {
		name, content, wantErr string
	}{
		{"no equals", "JUSTAKEY\n", "missing '='"},
		{"empty key", "=v\n", "empty key"},
		{"unterminated quote", `K="abc` + "\n", "unterminated"},
		{"bad escape", `K="a\d"` + "\n", `unsupported escape \d`},
		{"trailing garbage", `K="a" b` + "\n", "after closing quote"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeDotEnv(t, tc.content)
			_, err := DotEnvFile(path, "").Load()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestDotEnvFileOptional(t *testing.T) {
	got, err := DotEnvFile(filepath.Join(t.TempDir(), "missing.env"), "", Optional()).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}
