package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dvislobokov/sconf/secret"
)

// fakeVault — минимальный сервер Vault для тестов: отвечает заранее заданными
// данными на GET/POST /v1/<path> и запоминает пришедшие тела запросов.
type fakeVault struct {
	srv      *httptest.Server
	data     map[string]map[string]any // path -> data
	gotToken string
	gotBody  map[string]map[string]any // path -> тело POST
}

func newFakeVault(data map[string]map[string]any) *fakeVault {
	f := &fakeVault{data: data, gotBody: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		f.gotToken = r.Header.Get("X-Vault-Token")
		path := r.URL.Path[len("/v1/"):]
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.gotBody[path] = body
		}
		d, ok := f.data[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":["not found"]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": d})
	})
	f.srv = httptest.NewServer(mux)
	return f
}

// newFakeVaultFunc поднимает фейк, вычисляющий data динамически на каждый запрос
// (для тестов обновления). handler получает путь и возвращает поля секрета.
func newFakeVaultFunc(handler func(path string) map[string]any) *fakeVault {
	f := &fakeVault{gotBody: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		f.gotToken = r.Header.Get("X-Vault-Token")
		path := r.URL.Path[len("/v1/"):]
		_ = json.NewEncoder(w).Encode(map[string]any{"data": handler(path)})
	})
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *fakeVault) close() { f.srv.Close() }

// withEnv подменяет getenv на map для теста.
func withEnv(t *testing.T, env map[string]string) {
	t.Helper()
	prev := getenv
	getenv = func(k string) string { return env[k] }
	t.Cleanup(func() { getenv = prev })
}

type appConfig struct {
	DB   secret.UserPass
	Cert secret.Cert
}

func TestResolveTokenAuth(t *testing.T) {
	fv := newFakeVault(map[string]map[string]any{
		"database/creds/app": {"username": "v-app-1", "password": "pw"},
		"pki/issue/web":      {"certificate": "CERT", "private_key": "KEY"},
	})
	defer fv.close()

	withEnv(t, map[string]string{
		"VAULT_ADDR":  fv.srv.URL,
		"VAULT_TOKEN": "test-token",
	})

	cfg := appConfig{}
	if err := cfg.DB.UnmarshalConfig("database/creds/app"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Cert.UnmarshalConfig("pki/issue/web?common_name=app.example.com"); err != nil {
		t.Fatal(err)
	}

	if err := Resolve(context.Background(), &cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if cfg.DB.Username() != "v-app-1" || cfg.DB.Password() != "pw" {
		t.Fatalf("db = %q/%q", cfg.DB.Username(), cfg.DB.Password())
	}
	if cfg.Cert.Certificate() != "CERT" || cfg.Cert.PrivateKey() != "KEY" {
		t.Fatalf("cert: cert=%q key=%q", cfg.Cert.Certificate(), cfg.Cert.PrivateKey())
	}
	if fv.gotToken != "test-token" {
		t.Fatalf("token = %q", fv.gotToken)
	}
	// Параметры выпуска должны были уйти в теле POST.
	if cn := fv.gotBody["pki/issue/web"]["common_name"]; cn != "app.example.com" {
		t.Fatalf("common_name in body = %v", cn)
	}
}

func TestResolveMountPathPrefix(t *testing.T) {
	fv := newFakeVault(map[string]map[string]any{
		"team-a/database/creds/app": {"username": "u", "password": "p"},
	})
	defer fv.close()

	withEnv(t, map[string]string{
		"VAULT_ADDR":      fv.srv.URL,
		"VAULT_TOKEN":     "tok",
		"VAULT_MOUNTPATH": "team-a",
	})

	// VAULT_MOUNTPATH=team-a должен добавиться префиксом к пути секрета:
	// database/creds/app -> team-a/database/creds/app.
	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("database/creds/app")
	if err := Resolve(context.Background(), &cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.DB.Username() != "u" {
		t.Fatalf("username = %q", cfg.DB.Username())
	}
}

func TestResolveNoSecretsNoEnv(t *testing.T) {
	// Нет полей-секретов — Vault не требуется, ошибки быть не должно.
	withEnv(t, map[string]string{}) // пустое окружение
	type plain struct {
		Host string
		Port int
	}
	if err := Resolve(context.Background(), &plain{Host: "x"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestResolveSecretsButNotConfigured(t *testing.T) {
	withEnv(t, map[string]string{}) // нет VAULT_ADDR
	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("database/creds/app")
	err := Resolve(context.Background(), &cfg)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestResolveTokenAuthMissingToken(t *testing.T) {
	withEnv(t, map[string]string{"VAULT_ADDR": "http://127.0.0.1:8200"})
	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("database/creds/app")
	err := Resolve(context.Background(), &cfg)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestResolveSecretInSlice(t *testing.T) {
	fv := newFakeVault(map[string]map[string]any{
		"db/creds/a": {"username": "a", "password": "1"},
		"db/creds/b": {"username": "b", "password": "2"},
	})
	defer fv.close()
	withEnv(t, map[string]string{"VAULT_ADDR": fv.srv.URL, "VAULT_TOKEN": "t"})

	cfg := struct{ Creds []secret.UserPass }{Creds: make([]secret.UserPass, 2)}
	_ = cfg.Creds[0].UnmarshalConfig("db/creds/a")
	_ = cfg.Creds[1].UnmarshalConfig("db/creds/b")

	if err := Resolve(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Creds[0].Username() != "a" || cfg.Creds[1].Username() != "b" {
		t.Fatalf("got %q, %q", cfg.Creds[0].Username(), cfg.Creds[1].Username())
	}
}

func TestLoadConfigAuthMethods(t *testing.T) {
	base := map[string]string{"VAULT_ADDR": "http://v:8200"}
	cases := []struct {
		name string
		env  map[string]string
		ok   bool
	}{
		{"k8s ok", merge(base, map[string]string{"VAULT_AUTH": "kubernetes", "VAULT_K8S_ROLE": "app"}), true},
		{"k8s no role", merge(base, map[string]string{"VAULT_AUTH": "kubernetes"}), false},
		{"approle ok", merge(base, map[string]string{"VAULT_AUTH": "approle", "VAULT_ROLE_ID": "r", "VAULT_SECRET_ID": "s"}), true},
		{"approle no secret", merge(base, map[string]string{"VAULT_AUTH": "approle", "VAULT_ROLE_ID": "r"}), false},
		{"unknown", merge(base, map[string]string{"VAULT_AUTH": "ldap"}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, tc.env)
			_, err := loadConfig()
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
