// Command example/vault демонстрирует получение секретов из Vault. В конфиге
// (appsettings.yaml) заданы только ПУТИ до секретов, а значения приклад
// забирает из Vault при загрузке — прозрачно для остального кода.
//
// Чтобы пример был самодостаточным и запускался офлайн (go run ./example/vault),
// здесь поднимается игрушечный in-process «Vault», а переменные среды
// выставляются программно. В реальном приложении сервер и переменные среды
// (VAULT_ADDR, VAULT_TOKEN, VAULT_AUTH, ...) приходят из окружения — например
// из Kubernetes.
//
// Используется обычный sconf.Load — он сам заполняет секреты из Vault и
// запускает их фоновое обновление (AD/DB — раз в полчаса, pki — по TTL);
// обновление живёт, пока не отменён контекст (у Load — до конца процесса).
// Значения секретов читаются через методы (Username()/Password()/...), потому
// что фоновое обновление может менять их конкурентно.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dvislobokov/sconf"
	"github.com/dvislobokov/sconf/secret"
)

// Config — конфигурация приклада. Секреты объявляются типами из пакета secret;
// в YAML для них указывается путь до секрета в Vault.
type Config struct {
	App struct {
		Name string
	}
	DbCreds secret.UserPass `yaml:"db_cred"`
	WebCert secret.Cert     `yaml:"web_cert"`
	Extra   secret.KV       `yaml:"extra"`
	APIKey  secret.Value    `yaml:"api_key"`
}

func main() {
	// 1. Поднимаем игрушечный Vault и настраиваем окружение под него.
	stop := startFakeVault()
	defer stop()

	// 2. Загрузка конфигурации. sconf.Load сам сходит в Vault, заполнит секреты
	//    и запустит их фоновое обновление — оно живёт до конца процесса, наружу
	//    ничего останавливать не нужно. Если бы поля-секреты были, а окружение
	//    Vault не настроено, вернулась бы ошибка.
	cfg, err := sconf.Load[Config](
		sconf.New().AddYAMLFile(filepath.Join(sourceDir(), "appsettings.yaml")),
		os.Args[1:],
		sconf.WithSecretErrorHandler(func(err error) {
			fmt.Fprintln(os.Stderr, "vault refresh:", err)
		}),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	fmt.Printf("app:      %s\n", cfg.App.Name)
	fmt.Printf("db creds: user=%s password=%s  (из %s)\n",
		cfg.DbCreds.Username(), redact(cfg.DbCreds.Password()), cfg.DbCreds.Path())
	fmt.Printf("web cert: cn issued, serial=%s key=%s  (из %s)\n",
		cfg.WebCert.SerialNumber(), redact(cfg.WebCert.PrivateKey()), cfg.WebCert.Path())
	fmt.Printf("kv extra: region=%s tier=%s  (все ключи: %d)\n",
		cfg.Extra.Get("region"), cfg.Extra.Get("tier"), len(cfg.Extra.Values()))
	fmt.Printf("api key:  %s  (из поля api_key)\n", redact(cfg.APIKey.Get()))
	fmt.Println("refresh:  секреты обновляются в фоне автоматически")
}

// startFakeVault поднимает in-process HTTP-сервер, отвечающий как Vault, и
// выставляет VAULT_ADDR/VAULT_TOKEN. Возвращает функцию остановки.
func startFakeVault() func() {
	responses := map[string]map[string]any{
		"database/creds/app-role": {
			"username": "v-token-app-role-a1b2",
			"password": "s3cr3t-rotated-pw",
		},
		"pki/issue/web": {
			"certificate":   "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
			"private_key":   "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----",
			"issuing_ca":    "-----BEGIN CERTIFICATE-----\n...ca...\n-----END CERTIFICATE-----",
			"serial_number": "3f:1a:9c:...",
		},
		// KV v2 отдаёт поля под data, рядом metadata.
		"secret/data/billing": {
			"data": map[string]any{
				"region":  "eu-central-1",
				"tier":    "premium",
				"api_key": "ak_live_9f8e7d6c5b4a",
			},
			"metadata": map[string]any{"version": 2},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[len("/v1/"):]
		data, ok := responses[path]
		if !ok {
			http.Error(w, `{"errors":["not found"]}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))

	_ = os.Setenv("VAULT_ADDR", srv.URL)
	_ = os.Setenv("VAULT_TOKEN", "dev-root-token")
	// VAULT_AUTH по умолчанию token, поэтому больше ничего не нужно.

	return srv.Close
}

func redact(s string) string {
	if s == "" {
		return "(empty)"
	}
	return "***"
}

func sourceDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(file)
}
