package provider

import "strings"

// ArgsProvider читает аргументы командной строки. Поддерживаются формы:
//
//	--key=value   --key value   -key=value   /key=value   /key value
//
// В ключе "__" конвертируется в ":", так что --servers__0__host=a
// эквивалентно ключу servers:0:host. Позиционные аргументы игнорируются.
type ArgsProvider struct {
	args []string
}

// Args создаёт источник из среза аргументов (обычно os.Args[1:]).
func Args(args []string) *ArgsProvider {
	return &ArgsProvider{args: args}
}

func (p *ArgsProvider) Load() (map[string]string, error) {
	out := map[string]string{}
	for i := 0; i < len(p.args); i++ {
		a := p.args[i]

		var key string
		switch {
		case strings.HasPrefix(a, "--"):
			key = a[2:]
		case strings.HasPrefix(a, "-"):
			key = a[1:]
		case strings.HasPrefix(a, "/"):
			key = a[1:]
		default:
			continue // позиционный аргумент
		}
		if key == "" {
			continue
		}

		var val string
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			val = key[eq+1:]
			key = key[:eq]
		} else if i+1 < len(p.args) && !isFlag(p.args[i+1]) {
			val = p.args[i+1]
			i++
		}

		key = strings.ReplaceAll(key, "__", ":")
		out[key] = val
	}
	return out, nil
}

func isFlag(s string) bool {
	return strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/")
}
