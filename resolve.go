package sconf

import "context"

// SecretResolver дозаполняет уже связанную конфигурацию значениями из внешнего
// хранилища секретов (Vault). Реализация живёт в опциональном пакете
// sconf/vault и регистрируется через RegisterSecretResolver — ядро sconf не
// зависит ни от какого клиента секретов.
type SecretResolver interface {
	// Resolve обходит target (указатель на конфигурацию), находит поля-секреты
	// и заполняет их. Если полей-секретов нет, реализация обязана вернуть nil,
	// ничего не требуя от окружения.
	Resolve(ctx context.Context, target any) error
}

// secretResolver — активный резолвер, установленный пакетом sconf/vault
// через blank-import. nil, если пакет не подключён.
var secretResolver SecretResolver

// RegisterSecretResolver регистрирует резолвер секретов, вызываемый Load после
// бинда. Обычно вызывается из init() пакета-реализации (sconf/vault); прикладу
// достаточно blank-импорта:
//
//	import _ "github.com/dvislobokov/sconf/vault"
//
// Повторная регистрация заменяет предыдущий резолвер.
func RegisterSecretResolver(r SecretResolver) { secretResolver = r }

// resolveSecrets применяет зарегистрированный резолвер к target, если он есть.
func resolveSecrets(ctx context.Context, target any) error {
	if secretResolver == nil {
		return nil
	}
	return secretResolver.Resolve(ctx, target)
}
