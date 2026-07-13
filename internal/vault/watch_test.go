package vault

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvislobokov/sconf/secret"
	vault "github.com/hashicorp/vault-client-go"
)

func TestRefreshIntervalPolicy(t *testing.T) {
	up := &secret.UserPass{}
	cert := &secret.Cert{}

	cases := []struct {
		name  string
		s     secret.Resolvable
		req   secret.Request
		lease time.Duration
		want  time.Duration
	}{
		{
			name:  "explicit refresh wins",
			s:     up,
			req:   secret.Request{Refresh: 5 * time.Minute},
			lease: time.Hour,
			want:  5 * time.Minute,
		},
		{
			name: "userpass default half hour",
			s:    up,
			want: 30 * time.Minute,
		},
		{
			name:  "userpass short lease refreshes sooner",
			s:     up,
			lease: 10 * time.Minute, // 70% = 7m
			want:  7 * time.Minute,
		},
		{
			name:  "cert by ttl",
			s:     cert,
			lease: time.Hour, // 70% = 42m
			want:  42 * time.Minute,
		},
		{
			name: "cert without lease falls back to default",
			s:    cert,
			want: 30 * time.Minute,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := refreshInterval(tc.s, tc.req, tc.lease)
			if got != tc.want {
				t.Fatalf("interval = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLeaseTTLFromDataField(t *testing.T) {
	// Статические роли отдают ttl в данных при lease_duration=0.
	resp := &vault.Response[map[string]interface{}]{Data: map[string]any{"ttl": float64(600)}}
	if got := leaseTTL(resp); got != 10*time.Minute {
		t.Fatalf("leaseTTL = %s, want 10m", got)
	}
}

func TestWatcherRefreshesInBackground(t *testing.T) {
	// Сервер отдаёт разный пароль при каждом чтении — проверяем, что фоновое
	// обновление реально подхватывает новое значение.
	var reads atomic.Int64
	fv := newFakeVaultFunc(func(path string) map[string]any {
		n := reads.Add(1)
		return map[string]any{"username": "u", "password": pwSeq(n)}
	})
	defer fv.close()
	withEnv(t, map[string]string{"VAULT_ADDR": fv.srv.URL, "VAULT_TOKEN": "t"})

	cfg := struct {
		DB secret.UserPass
	}{}
	// Очень короткий интервал обновления, чтобы тест был быстрым.
	_ = cfg.DB.UnmarshalConfig("db/creds/app?refresh=20ms")

	ctx := context.Background()
	if err := Resolve(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	first := cfg.DB.Password()

	w, err := Watch(ctx, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	if w.Count() != 1 {
		t.Fatalf("watching %d, want 1", w.Count())
	}

	// Ждём, пока фон обновит пароль.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cfg.DB.Password() != first {
			return // обновилось
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("password not refreshed (still %q)", first)
}

func TestWatcherNoSecretsNoGoroutines(t *testing.T) {
	cfg := struct{ Host string }{Host: "x"}
	w, err := Watch(context.Background(), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if w.Count() != 0 {
		t.Fatalf("count = %d, want 0", w.Count())
	}
	w.Stop() // не должно блокировать
}

func pwSeq(n int64) string {
	return "pw-" + time.Duration(n).String()
}
