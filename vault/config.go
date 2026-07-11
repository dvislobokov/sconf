package vault

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ErrNotConfigured возвращается (через %w), когда в конфигурации есть
// поля-секреты, но окружение Vault не настроено (не задан адрес и т.п.).
var ErrNotConfigured = fmt.Errorf("vault: not configured")

// authMethod — способ аутентификации в Vault.
type authMethod string

const (
	authToken      authMethod = "token"
	authKubernetes authMethod = "kubernetes"
	authAppRole    authMethod = "approle"
)

// defaultK8sTokenPath — стандартный путь до service account токена в поде.
const defaultK8sTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// config — параметры подключения к Vault, собранные из переменных среды.
type config struct {
	Address   string
	Namespace string
	MountPath string // необязательный префикс пути ко всем секретам
	Timeout   time.Duration
	TLSSkip   bool

	Auth authMethod

	// token
	Token string

	// kubernetes
	K8sRole      string
	K8sMount     string
	K8sTokenPath string

	// approle
	RoleID       string
	SecretID     string
	AppRoleMount string
}

// getenv — источник переменных среды; подменяется в тестах.
var getenv = os.Getenv

// loadConfig собирает config из переменных среды. Возвращает ErrNotConfigured
// с пояснением, если обязательные для выбранного метода параметры не заданы.
//
// Переменные:
//
//	VAULT_ADDR / VAULT_URL   адрес сервера (обязательно)
//	VAULT_NAMESPACE          namespace (Vault Enterprise/HCP)
//	VAULT_MOUNTPATH          необязательный префикс пути ко всем секретам
//	VAULT_TIMEOUT            таймаут запроса (по умолчанию 30s)
//	VAULT_SKIP_VERIFY        отключить проверку TLS-сертификата
//	VAULT_AUTH               token | kubernetes | approle (по умолчанию token)
//	VAULT_TOKEN              токен (для auth=token)
//	VAULT_K8S_ROLE           роль (для auth=kubernetes)
//	VAULT_K8S_MOUNT          mount kubernetes-аутентификации (по умолчанию kubernetes)
//	VAULT_K8S_TOKEN_PATH     путь до SA-токена (по умолчанию стандартный путь в поде)
//	VAULT_ROLE_ID            role_id (для auth=approle)
//	VAULT_SECRET_ID          secret_id (для auth=approle)
//	VAULT_APPROLE_MOUNT      mount approle-аутентификации (по умолчанию approle)
func loadConfig() (config, error) {
	c := config{
		Address:      firstNonEmpty(getenv("VAULT_ADDR"), getenv("VAULT_URL")),
		Namespace:    getenv("VAULT_NAMESPACE"),
		MountPath:    strings.Trim(getenv("VAULT_MOUNTPATH"), "/"),
		TLSSkip:      isTrue(getenv("VAULT_SKIP_VERIFY")),
		Token:        getenv("VAULT_TOKEN"),
		K8sRole:      getenv("VAULT_K8S_ROLE"),
		K8sMount:     firstNonEmpty(getenv("VAULT_K8S_MOUNT"), "kubernetes"),
		K8sTokenPath: firstNonEmpty(getenv("VAULT_K8S_TOKEN_PATH"), defaultK8sTokenPath),
		RoleID:       getenv("VAULT_ROLE_ID"),
		SecretID:     getenv("VAULT_SECRET_ID"),
		AppRoleMount: firstNonEmpty(getenv("VAULT_APPROLE_MOUNT"), "approle"),
	}

	if c.Address == "" {
		return config{}, fmt.Errorf("%w: set VAULT_ADDR (or VAULT_URL) — "+
			"config has secret fields but Vault is not configured", ErrNotConfigured)
	}

	c.Timeout = 30 * time.Second
	if v := getenv("VAULT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("vault: invalid VAULT_TIMEOUT %q: %w", v, err)
		}
		c.Timeout = d
	}

	c.Auth = authMethod(strings.ToLower(firstNonEmpty(getenv("VAULT_AUTH"), string(authToken))))
	if err := validateAuth(c); err != nil {
		return config{}, err
	}
	return c, nil
}

// validateAuth проверяет, что для выбранного метода заданы нужные параметры.
func validateAuth(c config) error {
	switch c.Auth {
	case authToken:
		if c.Token == "" {
			return fmt.Errorf("%w: VAULT_AUTH=token requires VAULT_TOKEN", ErrNotConfigured)
		}
	case authKubernetes:
		if c.K8sRole == "" {
			return fmt.Errorf("%w: VAULT_AUTH=kubernetes requires VAULT_K8S_ROLE", ErrNotConfigured)
		}
	case authAppRole:
		if c.RoleID == "" || c.SecretID == "" {
			return fmt.Errorf("%w: VAULT_AUTH=approle requires VAULT_ROLE_ID and VAULT_SECRET_ID", ErrNotConfigured)
		}
	default:
		return fmt.Errorf("%w: unknown VAULT_AUTH %q (want token|kubernetes|approle)", ErrNotConfigured, c.Auth)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
