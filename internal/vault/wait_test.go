package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvislobokov/sconf/secret"
	vault "github.com/hashicorp/vault-client-go"
)

// newFlakyVault поднимает сервер, который первые fail запросов отвечает 503
// (как envoy при неготовом egress), а затем отдаёт data.
func newFlakyVault(fail int, data map[string]any) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(calls.Add(1)) <= fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"errors":["upstream connect error"]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	return srv, &calls
}

// flakyEnv — окружение для тестов ожидания. VAULT_MAX_RETRIES=-1 отключает
// внутренние ретраи HTTP-клиента (2 повтора с паузой ~1s), чтобы тесты
// проверяли именно цикл waitReady и не тянули секунды на паузах клиента.
func flakyEnv(t *testing.T, addr string, extra map[string]string) {
	env := map[string]string{
		"VAULT_ADDR":        addr,
		"VAULT_TOKEN":       "t",
		"VAULT_MAX_RETRIES": "-1",
	}
	for k, v := range extra {
		env[k] = v
	}
	withEnv(t, env)
}

func TestResolveWaitsForVault(t *testing.T) {
	srv, calls := newFlakyVault(2, map[string]any{"username": "u", "password": "p"})
	defer srv.Close()
	flakyEnv(t, srv.URL, nil)

	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("database/creds/app")

	err := Resolve(context.Background(), &cfg,
		WithWait(5*time.Second), WithWaitInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.DB.Username() != "u" {
		t.Fatalf("username = %q", cfg.DB.Username())
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestResolveNoWaitFailsFast(t *testing.T) {
	srv, calls := newFlakyVault(1000, nil)
	defer srv.Close()
	flakyEnv(t, srv.URL, nil)

	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("database/creds/app")

	if err := Resolve(context.Background(), &cfg); err == nil {
		t.Fatal("expected error without wait")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (no wait loop)", calls.Load())
	}
}

func TestResolveWaitFromEnv(t *testing.T) {
	srv, _ := newFlakyVault(2, map[string]any{"username": "u", "password": "p"})
	defer srv.Close()
	flakyEnv(t, srv.URL, map[string]string{
		"VAULT_WAIT":          "5s",
		"VAULT_WAIT_INTERVAL": "10ms",
	})

	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("database/creds/app")

	if err := Resolve(context.Background(), &cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.DB.Username() != "u" {
		t.Fatalf("username = %q", cfg.DB.Username())
	}
}

func TestResolveWaitGivesUp(t *testing.T) {
	srv, _ := newFlakyVault(1000, nil)
	defer srv.Close()
	flakyEnv(t, srv.URL, nil)

	cfg := struct{ DB secret.UserPass }{}
	_ = cfg.DB.UnmarshalConfig("database/creds/app")

	err := Resolve(context.Background(), &cfg,
		WithWait(50*time.Millisecond), WithWaitInterval(10*time.Millisecond))
	if err == nil {
		t.Fatal("expected error after wait timeout")
	}
	if !strings.Contains(err.Error(), "still unavailable") {
		t.Fatalf("error should mention waiting, got: %v", err)
	}
}

func TestKVProviderWaitsForVault(t *testing.T) {
	srv, calls := newFlakyVault(2, map[string]any{"host": "db.internal"})
	defer srv.Close()
	flakyEnv(t, srv.URL, map[string]string{
		"VAULT_WAIT":          "5s",
		"VAULT_WAIT_INTERVAL": "10ms",
	})

	got, err := KV("secret/data/app").Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["host"] != "db.internal" {
		t.Fatalf("host = %q", got["host"])
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestTemporary(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not configured", fmt.Errorf("x: %w", ErrNotConfigured), false},
		{"ctx canceled", context.Canceled, false},
		{"url error", &url.Error{Op: "Get", URL: "http://v", Err: errors.New("connection refused")}, true},
		{"503", &vault.ResponseError{StatusCode: http.StatusServiceUnavailable}, true},
		{"502", &vault.ResponseError{StatusCode: http.StatusBadGateway}, true},
		{"403", &vault.ResponseError{StatusCode: http.StatusForbidden}, false},
		{"404 wrapped", fmt.Errorf("fetch: %w", &vault.ResponseError{StatusCode: http.StatusNotFound}), false},
		{"plain", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := temporary(tc.err); got != tc.want {
				t.Fatalf("temporary(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestLoadWaitInvalidEnv(t *testing.T) {
	withEnv(t, map[string]string{"VAULT_WAIT": "nonsense"})
	if _, err := loadWait(waitOptions{}); err == nil {
		t.Fatal("expected error for invalid VAULT_WAIT")
	}
}

func TestRetryConfigFromEnvInvalid(t *testing.T) {
	withEnv(t, map[string]string{"VAULT_MAX_RETRIES": "many"})
	if _, _, err := retryConfigFromEnv(); err == nil {
		t.Fatal("expected error for invalid VAULT_MAX_RETRIES")
	}
}
