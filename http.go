package sconf

import (
	"fmt"
	"net/http"
)

// UsageHandler возвращает http.Handler, отдающий схему конфигурации типа T —
// какие переменные понимает сервис. Формат задаётся query-параметром format
// (table|env|json|yaml|toml, по умолчанию table), ответ — всегда голый текст
// (text/plain), без обёрток.
//
// envPrefix — префикс имён переменных среды (как у AddEnvironmentVariables);
// поле с тегом env показывается под своим именем без префикса.
//
// Хендлер стандартный, поэтому втыкается в любой роутер:
//
//	mux.Handle("/config/usage", sconf.UsageHandler[Config]("APP_"))       // net/http
//	r.GET("/config/usage", gin.WrapH(sconf.UsageHandler[Config]("APP_"))) // gin
//	e.GET("/config/usage", echo.WrapHandler(sconf.UsageHandler[Config]("APP_")))
//
// Отдаётся только схема (ключи, типы, default, enum, описания) — значений
// конфигурации в ответе нет.
func UsageHandler[T any](envPrefix string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out, err := UsageFormat[T](r.URL.Query().Get("format"), envPrefix)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, out)
	})
}
