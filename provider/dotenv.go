package provider

import (
	"fmt"
	"strings"
)

// DotEnvFile создаёт источник из .env-файла. Строки KEY=VALUE трактуются как
// переменные среды: prefix (может быть пустым) отсекается от имени, "__"
// превращается в разделитель уровней ":" — ровно как у Env. Реальное окружение
// процесса файл не трогает: это обычный слой конфигурации.
//
// Формат — привычный dotenv:
//
//	# комментарий
//	export APP_HOST=localhost      # необязательный export, хвостовой комментарий
//	APP_MESSAGE="hello\nworld"     # двойные кавычки: \n, \t, \", \\ раскрываются
//	APP_TOKEN='as $is'             # одинарные кавычки: значение как есть
//
// Многострочные значения и подстановка переменных не поддерживаются.
func DotEnvFile(path, prefix string, opts ...FileOption) *fileProvider {
	return newFileProvider(path, parseDotEnv(prefix), opts)
}

func parseDotEnv(prefix string) parseFunc {
	return func(data []byte) (map[string]string, error) {
		out := map[string]string{}
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.TrimRight(line, "\r"))
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if rest, ok := strings.CutPrefix(line, "export "); ok {
				line = strings.TrimSpace(rest)
			}
			eq := strings.IndexByte(line, '=')
			if eq < 0 {
				return nil, fmt.Errorf("dotenv: line %d: missing '='", i+1)
			}
			key := strings.TrimSpace(line[:eq])
			if key == "" {
				return nil, fmt.Errorf("dotenv: line %d: empty key", i+1)
			}
			val, err := dotenvValue(strings.TrimSpace(line[eq+1:]))
			if err != nil {
				return nil, fmt.Errorf("dotenv: line %d: %w", i+1, err)
			}

			if prefix != "" {
				if !strings.HasPrefix(key, prefix) {
					continue
				}
				key = key[len(prefix):]
			}
			if key == "" {
				continue
			}
			out[strings.ReplaceAll(key, "__", ":")] = val
		}
		return out, nil
	}
}

// dotenvValue разбирает значение: кавычки, экранирование, хвостовой комментарий.
func dotenvValue(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	switch s[0] {
	case '"', '\'':
		val, rest, err := unquote(s)
		if err != nil {
			return "", err
		}
		rest = strings.TrimSpace(rest)
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", fmt.Errorf("unexpected text after closing quote: %q", rest)
		}
		return val, nil
	default:
		// Без кавычек: значение до хвостового комментария (# после пробела).
		if idx := strings.Index(s, " #"); idx >= 0 {
			s = s[:idx]
		}
		return strings.TrimSpace(s), nil
	}
}

// unquote снимает кавычки с начала s и возвращает значение и остаток строки.
// В двойных кавычках раскрываются \n, \t, \r, \", \\; одинарные — литеральные.
func unquote(s string) (val, rest string, err error) {
	quote := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == quote:
			return b.String(), s[i+1:], nil
		case c == '\\' && quote == '"':
			i++
			if i >= len(s) {
				return "", "", fmt.Errorf("unterminated escape")
			}
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"', '\\':
				b.WriteByte(s[i])
			default:
				return "", "", fmt.Errorf("unsupported escape \\%c", s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", "", fmt.Errorf("unterminated quoted value")
}
