package secret

import "sync/atomic"

// UserPass — пара логин/пароль, получаемая из Vault чтением по ПОЛНОМУ пути
// (Method Read). Подходит для движков, у которых учётные данные читаются:
//
//   - database — динамические (database/creds/<role>) и статические
//     (database/static-creds/<role>) креды; в ответе username, password;
//   - openldap — статические роли (openldap/static-cred/<role>); в ответе
//     username, password;
//   - ad — статические креды (ad/static-cred/<role>); пароль лежит в
//     current_password (обрабатывается автоматически, см. ниже);
//   - kv/userpass и любые секреты с полями username/password.
//
// В конфиге указывается полный путь до секрета:
//
//	type Config struct {
//	    DbCreds secret.UserPass `yaml:"db_cred"`
//	}
//
//	# appsettings.yaml
//	db_cred: database/static-creds/app-role
//
// Логин берётся из поля "username". Пароль — из "current_password", если он есть
// (его отдаёт AD), иначе из "password" (database, openldap). Так оба случая
// разрешаются без ручной настройки. Для нестандартных движков имена полей можно
// переопределить параметрами пути:
//
//	custom: secret/path?username_field=login&password_field=secret
//
// Значения читаются потокобезопасно (методы Username/Password) — sconf.Load
// обновляет их в фоне автоматически. Интервал обновления по умолчанию —
// раз в 30 минут (или раньше, если lease короче); переопределяется ?refresh=.
type UserPass struct {
	refreshState
	ref  ref
	data atomic.Pointer[userPassData]
}

type userPassData struct {
	username string
	password string
}

// UnmarshalConfig принимает строковое значение из конфига (полный путь до
// секрета, опционально с параметрами username_field/password_field/refresh) и
// запоминает его для последующего резолвинга.
func (u *UserPass) UnmarshalConfig(value string) error {
	r, err := parseRef(value)
	if err != nil {
		return err
	}
	u.ref = r
	return nil
}

// SecretRequest сообщает резолверу, что креды нужно прочитать по полному пути.
func (u *UserPass) SecretRequest() Request {
	return Request{Method: Read, Path: u.ref.path, Refresh: u.ref.refreshParam()}
}

// Apply раскладывает ответ Vault в снапшот логина/пароля. Имена полей ответа
// берутся из username_field (по умолчанию "username") и правила выбора пароля
// (см. password). Потокобезопасно — заменяет снапшот целиком.
func (u *UserPass) Apply(data map[string]any) error {
	u.data.Store(&userPassData{
		username: asString(data[u.field("username_field", "username")]),
		password: u.password(data),
	})
	return nil
}

// password выбирает поле пароля из ответа. Явный password_field имеет приоритет;
// иначе сначала пробуется current_password (его возвращает AD), а затем
// password. Такой порядок однозначно разрешает оба распространённых случая: PG и
// openldap поля current_password не отдают вовсе, поэтому для них берётся
// password, а для AD — current_password, даже если в ответе присутствует и то,
// и другое.
func (u *UserPass) password(data map[string]any) string {
	if f := u.ref.params["password_field"]; f != "" {
		return asString(data[f])
	}
	if v, ok := data["current_password"]; ok {
		return asString(v)
	}
	return asString(data["password"])
}

// field возвращает имя поля ответа: переопределённое параметром пути либо def.
func (u *UserPass) field(param, def string) string {
	if v := u.ref.params[param]; v != "" {
		return v
	}
	return def
}

// Username возвращает текущий логин (потокобезопасно).
func (u *UserPass) Username() string {
	if d := u.data.Load(); d != nil {
		return d.username
	}
	return ""
}

// Password возвращает текущий пароль (потокобезопасно).
func (u *UserPass) Password() string {
	if d := u.data.Load(); d != nil {
		return d.password
	}
	return ""
}

// Resolved сообщает, был ли секрет успешно получен из Vault. Полезно для
// собственной валидации: если false, вероятно, путь до секрета не задан в
// конфиге.
func (u *UserPass) Resolved() bool { return u.data.Load() != nil }

// Path возвращает полный путь до секрета, как он задан в конфиге.
func (u *UserPass) Path() string { return u.ref.path }
