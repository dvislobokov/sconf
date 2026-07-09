package provider

import (
	"os"
	"strings"
)

// EnvProvider читает переменные среды. По соглашению
// Microsoft.Extensions.Configuration двойное подчёркивание "__" соответствует
// разделителю уровней ":", что позволяет маппить переменные в массивы объектов:
//
//	MYAPP_SERVERS__0__HOST=a  MYAPP_SERVERS__0__PORT=1
//	MYAPP_SERVERS__1__HOST=b  MYAPP_SERVERS__1__PORT=2
//
// с префиксом "MYAPP_" даёт servers:0:host=a, servers:0:port=1, ...
type EnvProvider struct {
	prefix string
	// environ подменяется в тестах; nil -> os.Environ.
	environ func() []string
}

// Env создаёт источник из переменных среды. prefix (может быть пустым)
// отсекается от имени переменной перед разбором.
func Env(prefix string) *EnvProvider {
	return &EnvProvider{prefix: prefix}
}

// WithEnviron задаёт источник переменных (для тестов).
func (e *EnvProvider) WithEnviron(fn func() []string) *EnvProvider {
	e.environ = fn
	return e
}

func (e *EnvProvider) Load() (map[string]string, error) {
	env := e.environ
	if env == nil {
		env = os.Environ
	}

	out := map[string]string{}
	for _, kv := range env() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key, val := kv[:eq], kv[eq+1:]

		if e.prefix != "" {
			if !strings.HasPrefix(key, e.prefix) {
				continue
			}
			key = key[len(e.prefix):]
		}
		if key == "" {
			continue
		}

		key = strings.ReplaceAll(key, "__", ":")
		out[key] = val
	}
	return out, nil
}
