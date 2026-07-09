package bind

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dvislobokov/sconf/internal/flat"
)

func TestDefaultTag(t *testing.T) {
	m := flat.New()
	m.Set("port", "9090") // host отсутствует -> берётся default
	var v struct {
		Host string `default:"0.0.0.0"`
		Port int    `default:"8080"`
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Host != "0.0.0.0" {
		t.Errorf("Host=%q, want default 0.0.0.0", v.Host)
	}
	if v.Port != 9090 {
		t.Errorf("Port=%d, want 9090 (источник перекрывает default)", v.Port)
	}
}

func TestEnumValid(t *testing.T) {
	m := flat.New()
	m.Set("level", "INFO") // регистр не важен -> канонизируется в "info"
	var v struct {
		Level string `enum:"debug,info,warn,error"`
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Level != "info" {
		t.Errorf("Level=%q, want canonical info", v.Level)
	}
}

func TestEnumInvalid(t *testing.T) {
	m := flat.New()
	m.Set("level", "trace")
	var v struct {
		Level string `enum:"debug,info,warn,error"`
	}
	err := Bind(m, "", &v)
	if !errors.Is(err, ErrEnum) {
		t.Fatalf("errors.Is(ErrEnum) = false: %v", err)
	}
	if !strings.Contains(err.Error(), "trace") || !strings.Contains(err.Error(), "debug, info") {
		t.Errorf("ошибка должна содержать значение и список: %v", err)
	}
}

func TestEnumDefaultCombined(t *testing.T) {
	m := flat.New() // значения нет -> default "dev", проходит enum
	var v struct {
		Mode string `enum:"dev,prod" default:"dev"`
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Mode != "dev" {
		t.Errorf("Mode=%q", v.Mode)
	}
}

type usageSettings struct {
	Host   string `description:"listen host" default:"0.0.0.0"`
	Port   int    `json:"port" default:"8080" usage:"listen port"`
	Mode   string `enum:"dev,prod" default:"dev" description:"run mode"`
	Nested struct {
		Retries int `default:"3"`
	}
	Servers []struct {
		Host string
	}
}

func TestDescribe(t *testing.T) {
	entries := Describe(reflect.TypeOf(usageSettings{}))
	got := map[string]Entry{}
	for _, e := range entries {
		got[e.Key] = e
	}

	// Ключи сохраняют регистр полей/тегов (как пути в ошибках биндера).
	if e := got["Host"]; e.Default != "0.0.0.0" || !e.HasDefault || e.Description != "listen host" || e.Type != "string" {
		t.Errorf("host entry: %+v", e)
	}
	if e := got["port"]; e.Default != "8080" || e.Description != "listen port" || e.Type != "int" {
		t.Errorf("port entry: %+v", e)
	}
	if e := got["Mode"]; len(e.Enum) != 2 || e.Enum[0] != "dev" || e.Description != "run mode" {
		t.Errorf("mode entry: %+v", e)
	}
	if _, ok := got["Nested:Retries"]; !ok {
		t.Errorf("вложенный ключ Nested:Retries отсутствует: %v", keys(entries))
	}
	if _, ok := got["Servers:N:Host"]; !ok {
		t.Errorf("ключ элемента массива Servers:N:Host отсутствует: %v", keys(entries))
	}
}

func TestUsageString(t *testing.T) {
	out := Usage(reflect.TypeOf(usageSettings{}))
	for _, want := range []string{"--Host", "--Mode", "{dev|prod}", "(default \"8080\")", "run mode"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage не содержит %q:\n%s", want, out)
		}
	}
}

func keys(e []Entry) []string {
	var out []string
	for _, x := range e {
		out = append(out, x.Key)
	}
	return out
}
