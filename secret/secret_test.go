package secret

import (
	"reflect"
	"testing"
)

func TestUserPassUnmarshalAndRequest(t *testing.T) {
	var u UserPass
	if err := u.UnmarshalConfig("  database/creds/app-role  "); err != nil {
		t.Fatalf("UnmarshalConfig: %v", err)
	}
	if u.Path() != "database/creds/app-role" {
		t.Fatalf("path = %q", u.Path())
	}
	req := u.SecretRequest()
	if req.Method != Read || req.Path != "database/creds/app-role" {
		t.Fatalf("req = %+v", req)
	}
	if u.Resolved() {
		t.Fatal("must not be resolved before Apply")
	}
}

func TestUserPassApplyDatabase(t *testing.T) {
	var u UserPass
	if err := u.Apply(map[string]any{"username": "v-app-x", "password": "s3cr3t"}); err != nil {
		t.Fatal(err)
	}
	if u.Username() != "v-app-x" || u.Password() != "s3cr3t" {
		t.Fatalf("got %q/%q", u.Username(), u.Password())
	}
	if !u.Resolved() {
		t.Fatal("must be resolved after Apply")
	}
}

func TestUserPassADCurrentPasswordFirst(t *testing.T) {
	// AD static: в ответе есть и current_password, и password. По умолчанию
	// current_password имеет приоритет — берётся он, а не password.
	var u UserPass
	_ = u.UnmarshalConfig("ad/static-cred/svc")
	err := u.Apply(map[string]any{
		"username":         "svc",
		"current_password": "rotated",
		"password":         "SHOULD-BE-IGNORED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Password() != "rotated" {
		t.Fatalf("password = %q, want rotated", u.Password())
	}
}

func TestUserPassDefaultFallsBackToPassword(t *testing.T) {
	// PG/openldap: current_password нет — берём password.
	var u UserPass
	_ = u.UnmarshalConfig("database/static-creds/app")
	if err := u.Apply(map[string]any{"username": "u", "password": "p"}); err != nil {
		t.Fatal(err)
	}
	if u.Password() != "p" {
		t.Fatalf("password = %q", u.Password())
	}
}

func TestUserPassPasswordFieldOverride(t *testing.T) {
	// Явный password_field имеет приоритет над эвристикой current_password.
	var u UserPass
	_ = u.UnmarshalConfig("secret/path?password_field=secret&username_field=login")
	err := u.Apply(map[string]any{
		"login":            "u",
		"secret":           "wanted",
		"current_password": "SHOULD-BE-IGNORED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Username() != "u" || u.Password() != "wanted" {
		t.Fatalf("got %q/%q", u.Username(), u.Password())
	}
}

func TestUserPassFromKVFieldJSON(t *testing.T) {
	var u UserPass
	_ = u.UnmarshalConfig("A/APP/OSH/KV/secrets?field=redis")
	err := u.Apply(map[string]any{
		"redis": `{"username": "svc", "password": "pw-json"}`,
		"other": "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Username() != "svc" || u.Password() != "pw-json" {
		t.Fatalf("got %q/%q", u.Username(), u.Password())
	}
}

func TestUserPassFromKVFieldYAML(t *testing.T) {
	var u UserPass
	_ = u.UnmarshalConfig("kv/secrets?field=redis")
	if err := u.Apply(map[string]any{"redis": "username: svc\npassword: pw-yaml\n"}); err != nil {
		t.Fatal(err)
	}
	if u.Username() != "svc" || u.Password() != "pw-yaml" {
		t.Fatalf("got %q/%q", u.Username(), u.Password())
	}
}

func TestUserPassFromKVFieldTOML(t *testing.T) {
	var u UserPass
	_ = u.UnmarshalConfig("kv/secrets?field=redis")
	if err := u.Apply(map[string]any{"redis": "username = \"svc\"\npassword = \"pw-toml\"\n"}); err != nil {
		t.Fatal(err)
	}
	if u.Username() != "svc" || u.Password() != "pw-toml" {
		t.Fatalf("got %q/%q", u.Username(), u.Password())
	}
}

func TestUserPassFromKVFieldWithOverrides(t *testing.T) {
	// username_field/password_field применяются к разобранному содержимому поля.
	var u UserPass
	_ = u.UnmarshalConfig("kv/secrets?field=redis&username_field=login&password_field=secret")
	if err := u.Apply(map[string]any{"redis": `{"login": "u", "secret": "p"}`}); err != nil {
		t.Fatal(err)
	}
	if u.Username() != "u" || u.Password() != "p" {
		t.Fatalf("got %q/%q", u.Username(), u.Password())
	}
}

func TestUserPassFromKVFieldKVv2(t *testing.T) {
	// Обёртка KV v2 (data/metadata) снимается перед выбором поля.
	var u UserPass
	_ = u.UnmarshalConfig("secret/data/app?field=redis")
	err := u.Apply(map[string]any{
		"data":     map[string]any{"redis": `{"username": "svc", "password": "pw"}`},
		"metadata": map[string]any{"version": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Username() != "svc" || u.Password() != "pw" {
		t.Fatalf("got %q/%q", u.Username(), u.Password())
	}
}

func TestUserPassFromKVFieldMissing(t *testing.T) {
	var u UserPass
	_ = u.UnmarshalConfig("kv/secrets?field=nope")
	err := u.Apply(map[string]any{"redis": "{}"})
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestUserPassFromKVFieldBadText(t *testing.T) {
	// Голый текст, не являющийся отображением ни в одном формате, — ошибка.
	var u UserPass
	_ = u.UnmarshalConfig("kv/secrets?field=redis")
	if err := u.Apply(map[string]any{"redis": "just a plain string"}); err == nil {
		t.Fatal("expected error for unparsable field text")
	}
	if err := u.Apply(map[string]any{"redis": ""}); err == nil {
		t.Fatal("expected error for empty field text")
	}
}

func TestUserPassPlain(t *testing.T) {
	cases := map[string]string{
		"json": `plain:{"username": "u", "password": "p"}`,
		"yaml": "plain:username: u\npassword: p\n",
		"toml": "plain:username = \"u\"\npassword = \"p\"\n",
	}
	for name, value := range cases {
		var u UserPass
		if err := u.UnmarshalConfig(value); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !u.Resolved() {
			t.Fatalf("%s: must be resolved right after UnmarshalConfig", name)
		}
		if u.Username() != "u" || u.Password() != "p" {
			t.Fatalf("%s: got %q/%q", name, u.Username(), u.Password())
		}
		if u.SecretRequest().Path != "" {
			t.Fatalf("%s: plain secret must not request Vault", name)
		}
	}
}

func TestUserPassPlainBadText(t *testing.T) {
	var u UserPass
	if err := u.UnmarshalConfig("plain:just text"); err == nil {
		t.Fatal("expected error for unparsable plain value")
	}
	if err := u.UnmarshalConfig("plain:"); err == nil {
		t.Fatal("expected error for empty plain value")
	}
}

func TestValuePlain(t *testing.T) {
	var v Value
	// Значение берётся как есть, без разбора — даже если похоже на путь или JSON.
	if err := v.UnmarshalConfig("plain:sk_test_51?not=parsed"); err != nil {
		t.Fatal(err)
	}
	if !v.Resolved() || v.Get() != "sk_test_51?not=parsed" {
		t.Fatalf("got %q resolved=%v", v.Get(), v.Resolved())
	}
}

func TestKVPlain(t *testing.T) {
	var k KV
	if err := k.UnmarshalConfig(`plain:{"region": "local", "tier": "dev"}`); err != nil {
		t.Fatal(err)
	}
	if k.Get("region") != "local" || k.Get("tier") != "dev" {
		t.Fatalf("got %v", k.Values())
	}
}

func TestCertPlain(t *testing.T) {
	var c Cert
	err := c.UnmarshalConfig("plain:certificate: CERT\nprivate_key: KEY\nserial_number: dev-01\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Certificate() != "CERT" || c.PrivateKey() != "KEY" || c.SerialNumber() != "dev-01" {
		t.Fatalf("got %q/%q/%q", c.Certificate(), c.PrivateKey(), c.SerialNumber())
	}
}

func TestCertUnmarshalWithParams(t *testing.T) {
	var c Cert
	if err := c.UnmarshalConfig("pki/issue/web?common_name=app.example.com&ttl=24h"); err != nil {
		t.Fatal(err)
	}
	req := c.SecretRequest()
	if req.Method != Write {
		t.Fatalf("method = %d", req.Method)
	}
	if req.Path != "pki/issue/web" {
		t.Fatalf("path = %q", req.Path)
	}
	want := map[string]any{"common_name": "app.example.com", "ttl": "24h"}
	if !reflect.DeepEqual(req.Data, want) {
		t.Fatalf("data = %+v, want %+v", req.Data, want)
	}
}

func TestCertApply(t *testing.T) {
	var c Cert
	err := c.Apply(map[string]any{
		"certificate":   "CERT",
		"private_key":   "KEY",
		"issuing_ca":    "CA",
		"ca_chain":      []any{"CA1", "CA2"},
		"serial_number": "01:02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Certificate() != "CERT" || c.PrivateKey() != "KEY" || c.IssuingCA() != "CA" {
		t.Fatalf("got cert=%q key=%q ca=%q", c.Certificate(), c.PrivateKey(), c.IssuingCA())
	}
	if !reflect.DeepEqual(c.CAChain(), []string{"CA1", "CA2"}) {
		t.Fatalf("ca_chain = %v", c.CAChain())
	}
	if c.SerialNumber() != "01:02" {
		t.Fatalf("serial = %q", c.SerialNumber())
	}
}

func TestParseRefErrors(t *testing.T) {
	for _, in := range []string{"", "   ", "/", "?a=b"} {
		if _, err := parseRef(in); err == nil {
			t.Errorf("parseRef(%q) expected error", in)
		}
	}
}

func TestKVAllKeysV2Unwrap(t *testing.T) {
	var k KV
	_ = k.UnmarshalConfig("secret/data/myapp")
	// Ответ KV v2: поля лежат под data, рядом metadata.
	err := k.Apply(map[string]any{
		"data":     map[string]any{"host": "db.internal", "port": float64(5432)},
		"metadata": map[string]any{"version": float64(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if k.Get("host") != "db.internal" || k.Get("port") != "5432" {
		t.Fatalf("values = %v", k.Values())
	}
	if _, ok := k.Values()["metadata"]; ok {
		t.Fatal("metadata must not leak into Values")
	}
}

func TestKVv1NoUnwrap(t *testing.T) {
	var k KV
	_ = k.UnmarshalConfig("secret/myapp")
	if err := k.Apply(map[string]any{"a": "1", "b": "2"}); err != nil {
		t.Fatal(err)
	}
	if len(k.Values()) != 2 || k.Get("a") != "1" {
		t.Fatalf("values = %v", k.Values())
	}
}

func TestValueByField(t *testing.T) {
	var v Value
	_ = v.UnmarshalConfig("secret/data/myapp?field=api_key")
	err := v.Apply(map[string]any{"data": map[string]any{"api_key": "xyz", "other": "n"}, "metadata": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Get() != "xyz" {
		t.Fatalf("value = %q", v.Get())
	}
}

func TestValueSingleFieldImplicit(t *testing.T) {
	var v Value
	_ = v.UnmarshalConfig("secret/myapp")
	if err := v.Apply(map[string]any{"only": "here"}); err != nil {
		t.Fatal(err)
	}
	if v.Get() != "here" {
		t.Fatalf("value = %q", v.Get())
	}
}

func TestValueAmbiguousWithoutField(t *testing.T) {
	var v Value
	_ = v.UnmarshalConfig("secret/myapp")
	err := v.Apply(map[string]any{"a": "1", "b": "2"})
	if err == nil {
		t.Fatal("expected error for ambiguous field")
	}
}

func TestValueMissingField(t *testing.T) {
	var v Value
	_ = v.UnmarshalConfig("secret/myapp?field=nope")
	if err := v.Apply(map[string]any{"a": "1"}); err == nil {
		t.Fatal("expected error for missing field")
	}
}

// Все типы секретов должны реализовывать Resolvable (через указатель).
func TestImplementsResolvable(t *testing.T) {
	var _ Resolvable = (*UserPass)(nil)
	var _ Resolvable = (*Cert)(nil)
	var _ Resolvable = (*KV)(nil)
	var _ Resolvable = (*Value)(nil)
}
