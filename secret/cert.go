package secret

import "sync/atomic"

// Cert — сертификат и закрытый ключ, выпускаемые движком pki Vault. Выпуск —
// это запись с параметрами (Method Write) по пути pki/issue/<role>:
//
//	type Config struct {
//	    WebCert secret.Cert `yaml:"web_cert"`
//	}
//
//	# appsettings.yaml — путь + параметры выпуска в стиле query-строки
//	web_cert: pki/issue/web?common_name=app.example.com&ttl=24h
//
// Любые параметры после "?" (кроме управляющих, например refresh) передаются в
// тело запроса Vault как есть (common_name, alt_names, ip_sans, ttl, format).
//
// Значения читаются потокобезопасно (методы Certificate/PrivateKey/...) — их
// можно перевыпускать в фоне (см. sconf/vault.Watch). По умолчанию сертификат
// перевыпускается по TTL (около 70% срока действия); переопределяется ?refresh=.
type Cert struct {
	refreshState
	ref  ref
	data atomic.Pointer[certData]
}

type certData struct {
	certificate  string
	privateKey   string
	issuingCA    string
	caChain      []string
	serialNumber string
}

// UnmarshalConfig принимает путь и параметры выпуска сертификата.
func (c *Cert) UnmarshalConfig(value string) error {
	r, err := parseRef(value)
	if err != nil {
		return err
	}
	c.ref = r
	return nil
}

// SecretRequest сообщает резолверу выполнить запись (issue) с параметрами.
func (c *Cert) SecretRequest() Request {
	return Request{Method: Write, Path: c.ref.path, Data: c.ref.dataMap(), Refresh: c.ref.refreshParam()}
}

// Apply раскладывает ответ pki/issue в снапшот сертификата (потокобезопасно).
func (c *Cert) Apply(data map[string]any) error {
	c.data.Store(&certData{
		certificate:  asString(data["certificate"]),
		privateKey:   asString(data["private_key"]),
		issuingCA:    asString(data["issuing_ca"]),
		caChain:      asStrings(data["ca_chain"]),
		serialNumber: asString(data["serial_number"]),
	})
	return nil
}

// Certificate возвращает PEM выпущенного сертификата (потокобезопасно).
func (c *Cert) Certificate() string { return c.get(func(d *certData) string { return d.certificate }) }

// PrivateKey возвращает PEM закрытого ключа.
func (c *Cert) PrivateKey() string { return c.get(func(d *certData) string { return d.privateKey }) }

// IssuingCA возвращает PEM выпускающего CA.
func (c *Cert) IssuingCA() string { return c.get(func(d *certData) string { return d.issuingCA }) }

// SerialNumber возвращает серийный номер сертификата.
func (c *Cert) SerialNumber() string {
	return c.get(func(d *certData) string { return d.serialNumber })
}

// CAChain возвращает цепочку CA.
func (c *Cert) CAChain() []string {
	if d := c.data.Load(); d != nil {
		return d.caChain
	}
	return nil
}

func (c *Cert) get(f func(*certData) string) string {
	if d := c.data.Load(); d != nil {
		return f(d)
	}
	return ""
}

// Resolved сообщает, был ли сертификат успешно выпущен.
func (c *Cert) Resolved() bool { return c.data.Load() != nil }

// Path возвращает путь выпуска, как он задан в конфиге.
func (c *Cert) Path() string { return c.ref.path }
