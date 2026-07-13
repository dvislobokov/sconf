package vault

import (
	"context"
	"fmt"
	"os"
	"strings"

	vault "github.com/hashicorp/vault-client-go"
	"github.com/hashicorp/vault-client-go/schema"
)

// authenticate устанавливает на клиенте токен согласно выбранному методу.
// Для token это просто установка VAULT_TOKEN; для kubernetes/approle —
// логин в соответствующий auth-движок и установка полученного client_token.
func authenticate(ctx context.Context, client *vault.Client, c config) error {
	switch c.Auth {
	case authToken:
		return client.SetToken(c.Token)

	case authKubernetes:
		jwt, err := os.ReadFile(c.K8sTokenPath)
		if err != nil {
			return fmt.Errorf("vault: read k8s service account token %q: %w", c.K8sTokenPath, err)
		}
		resp, err := client.Auth.KubernetesLogin(ctx,
			schema.KubernetesLoginRequest{
				Role: c.K8sRole,
				Jwt:  strings.TrimSpace(string(jwt)),
			},
			vault.WithMountPath(c.K8sMount),
		)
		if err != nil {
			return fmt.Errorf("vault: kubernetes login (role %q): %w", c.K8sRole, err)
		}
		return setLoginToken(client, resp)

	case authAppRole:
		resp, err := client.Auth.AppRoleLogin(ctx,
			schema.AppRoleLoginRequest{
				RoleId:   c.RoleID,
				SecretId: c.SecretID,
			},
			vault.WithMountPath(c.AppRoleMount),
		)
		if err != nil {
			return fmt.Errorf("vault: approle login: %w", err)
		}
		return setLoginToken(client, resp)

	default:
		return fmt.Errorf("%w: unknown auth method %q", ErrNotConfigured, c.Auth)
	}
}

// setLoginToken извлекает client_token из ответа логина и ставит его на клиент.
func setLoginToken(client *vault.Client, resp *vault.Response[map[string]interface{}]) error {
	if resp == nil || resp.Auth == nil || resp.Auth.ClientToken == "" {
		return fmt.Errorf("vault: login succeeded but no client token returned")
	}
	return client.SetToken(resp.Auth.ClientToken)
}
