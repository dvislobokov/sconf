package vault

import (
	"testing"
)

func TestKVProviderFlattensIntoConfig(t *testing.T) {
	// KV v2: поля лежат под data. Внутри — скаляр, вложенный объект и список.
	fv := newFakeVault(map[string]map[string]any{
		"secret/data/myapp": {
			"data": map[string]any{
				"log_level": "debug",
				"db": map[string]any{
					"host": "db.internal",
					"port": float64(5432),
				},
				"hosts": []any{"a", "b"},
			},
			"metadata": map[string]any{"version": float64(1)},
		},
	})
	defer fv.close()
	withEnv(t, map[string]string{"VAULT_ADDR": fv.srv.URL, "VAULT_TOKEN": "t"})

	out, err := KV("secret/data/myapp").Load()
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"log_level": "debug",
		"db:host":   "db.internal",
		"db:port":   "5432",
		"hosts:0":   "a",
		"hosts:1":   "b",
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("out[%q] = %q, want %q", k, out[k], v)
		}
	}
	if _, ok := out["metadata:version"]; ok {
		t.Error("metadata must not leak into config")
	}
}

func TestKVProviderAtPrefix(t *testing.T) {
	fv := newFakeVault(map[string]map[string]any{
		"secret/db": {"host": "h", "port": float64(1)}, // KV v1 (без обёртки)
	})
	defer fv.close()
	withEnv(t, map[string]string{"VAULT_ADDR": fv.srv.URL, "VAULT_TOKEN": "t"})

	out, err := KV("secret/db", At("database")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if out["database:host"] != "h" || out["database:port"] != "1" {
		t.Fatalf("out = %v", out)
	}
}

func TestKVProviderNotConfigured(t *testing.T) {
	withEnv(t, map[string]string{}) // нет VAULT_ADDR
	if _, err := KV("secret/data/x").Load(); err == nil {
		t.Fatal("expected error when Vault not configured")
	}
}
