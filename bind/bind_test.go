package bind

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dvislobokov/sconf/internal/flat"
)

func mapOf(kv map[string]string) *flat.Map {
	m := flat.New()
	m.SetAll(kv)
	return m
}

func TestBindScalars(t *testing.T) {
	m := mapOf(map[string]string{
		"s": "hello", "i": "-7", "u": "42", "f": "3.14", "b": "true", "d": "1500ms",
	})
	var v struct {
		S string
		I int
		U uint
		F float64
		B bool
		D time.Duration
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.S != "hello" || v.I != -7 || v.U != 42 || v.F != 3.14 || !v.B || v.D != 1500*time.Millisecond {
		t.Errorf("got %+v", v)
	}
}

func TestBindSliceWithHoles(t *testing.T) {
	// Индексы 0 и 2 (дыра на 1) должны схлопнуться в два элемента по порядку.
	m := mapOf(map[string]string{
		"servers:0:host": "a", "servers:0:port": "1",
		"servers:2:host": "c", "servers:2:port": "3",
	})
	var v struct {
		Servers []struct {
			Host string
			Port int
		}
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Servers) != 2 {
		t.Fatalf("len=%d, want 2: %+v", len(v.Servers), v.Servers)
	}
	if v.Servers[0].Host != "a" || v.Servers[1].Host != "c" || v.Servers[1].Port != 3 {
		t.Errorf("got %+v", v.Servers)
	}
}

func TestBindMapAndPointer(t *testing.T) {
	m := mapOf(map[string]string{
		"tags:env": "prod", "tags:region": "eu", "ptr": "9",
	})
	var v struct {
		Tags map[string]string
		Ptr  *int
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Tags["env"] != "prod" || v.Tags["region"] != "eu" {
		t.Errorf("tags=%+v", v.Tags)
	}
	if v.Ptr == nil || *v.Ptr != 9 {
		t.Errorf("ptr=%v", v.Ptr)
	}
}

func TestBindTagPriority(t *testing.T) {
	// json/yaml/toml/name — в этом порядке приоритета.
	m := mapOf(map[string]string{
		"j": "jv", "y": "yv", "n": "nv", "-skip-parent:ignored": "x",
	})
	var v struct {
		A string `json:"j"`
		B string `yaml:"y"`
		C string `name:"n"`
		D string `json:"-"`
	}
	m.Set("d", "should-be-ignored")
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.A != "jv" || v.B != "yv" || v.C != "nv" {
		t.Errorf("got %+v", v)
	}
	if v.D != "" {
		t.Errorf("D должно быть пропущено (json:\"-\"), got %q", v.D)
	}
}

func TestBindTypeError(t *testing.T) {
	m := mapOf(map[string]string{"port": "abc"})
	var v struct{ Port int }
	err := Bind(m, "", &v)
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if !errors.Is(err, ErrBindType) {
		t.Errorf("errors.Is(ErrBindType) = false: %v", err)
	}
	if !strings.Contains(err.Error(), `"Port"`) || !strings.Contains(err.Error(), `"abc"`) {
		t.Errorf("ошибка должна содержать путь и значение: %v", err)
	}
}

type upperString struct{ v string }

func (u *upperString) UnmarshalConfig(value string) error {
	u.v = strings.ToUpper(value)
	return nil
}

func TestBindUnmarshaler(t *testing.T) {
	m := mapOf(map[string]string{"name": "abc"})
	var v struct{ Name upperString }
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Name.v != "ABC" {
		t.Errorf("UnmarshalConfig не вызван: %+v", v)
	}
}

// fakeSecret имитирует тип секрета: строка — «путь», секция — готовые данные.
type fakeSecret struct {
	path string
	data map[string]any
}

func (f *fakeSecret) UnmarshalConfig(value string) error {
	f.path = value
	return nil
}

func (f *fakeSecret) Apply(data map[string]any) error {
	if _, bad := data["boom"]; bad {
		return errors.New("boom")
	}
	f.data = data
	return nil
}

func TestBindApplierNestedSection(t *testing.T) {
	m := mapOf(map[string]string{
		"cred:username":     "u",
		"cred:password":     "p",
		"cred:chain:0":      "a",
		"cred:chain:1":      "b",
		"cred:extra:region": "local",
	})
	var v struct {
		Cred fakeSecret `yaml:"cred"`
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Cred.path != "" {
		t.Errorf("UnmarshalConfig не должен вызываться для секции: %q", v.Cred.path)
	}
	if v.Cred.data["username"] != "u" || v.Cred.data["password"] != "p" {
		t.Errorf("data = %+v", v.Cred.data)
	}
	if chain, ok := v.Cred.data["chain"].([]any); !ok || len(chain) != 2 || chain[0] != "a" {
		t.Errorf("chain = %+v", v.Cred.data["chain"])
	}
	if extra, ok := v.Cred.data["extra"].(map[string]any); !ok || extra["region"] != "local" {
		t.Errorf("extra = %+v", v.Cred.data["extra"])
	}
}

func TestBindApplierSectionBeatsScalar(t *testing.T) {
	// И путь-скаляр, и секция: побеждает секция (локальный слой переопределяет
	// боевой путь готовыми значениями).
	m := mapOf(map[string]string{
		"cred":          "database/creds/app",
		"cred:username": "u",
	})
	var v struct {
		Cred fakeSecret `yaml:"cred"`
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Cred.path != "" || v.Cred.data["username"] != "u" {
		t.Errorf("секция должна победить: path=%q data=%+v", v.Cred.path, v.Cred.data)
	}
}

func TestBindApplierScalarStillWorks(t *testing.T) {
	m := mapOf(map[string]string{"cred": "database/creds/app"})
	var v struct {
		Cred fakeSecret `yaml:"cred"`
	}
	if err := Bind(m, "", &v); err != nil {
		t.Fatal(err)
	}
	if v.Cred.path != "database/creds/app" || v.Cred.data != nil {
		t.Errorf("скаляр должен идти через UnmarshalConfig: %+v", v.Cred)
	}
}

func TestBindApplierError(t *testing.T) {
	m := mapOf(map[string]string{"cred:boom": "x"})
	var v struct {
		Cred fakeSecret `yaml:"cred"`
	}
	err := Bind(m, "", &v)
	if err == nil || !errors.Is(err, ErrBindType) {
		t.Fatalf("ожидалась ошибка ErrBindType, got %v", err)
	}
}

type mustHavePort struct{ Port int }

func (s *mustHavePort) Validate() error {
	if s.Port == 0 {
		return errors.New("port required")
	}
	return nil
}

func TestBindValidator(t *testing.T) {
	ok := mapOf(map[string]string{"port": "8080"})
	var good mustHavePort
	if err := Bind(ok, "", &good); err != nil {
		t.Fatalf("не ожидалась ошибка: %v", err)
	}

	bad := mapOf(map[string]string{"other": "x"})
	var v mustHavePort
	err := Bind(bad, "", &v)
	if err == nil || !strings.Contains(err.Error(), "port required") {
		t.Errorf("ожидалась ошибка валидации, got %v", err)
	}
}

func TestBindNilTargetPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ожидалась паника на nil target")
		}
	}()
	var p *struct{ X int }
	_ = Bind(flat.New(), "", p)
}
