package sconf

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveUsage(t *testing.T, method, target string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	UsageHandler[helpSettings]("APP_").ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestUsageHandlerDefaultTable(t *testing.T) {
	resp, body := serveUsage(t, http.MethodGet, "/config/usage")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q", ct)
	}
	if body != Usage[helpSettings]() {
		t.Fatalf("body != Usage:\n%s", body)
	}
}

func TestUsageHandlerFormats(t *testing.T) {
	cases := []struct {
		format string
		want   string // подстрока в теле
	}{
		{"env", "APP_HOST=0.0.0.0"},
		{"json", `"env": "DB_HOST"`},
		{"yaml", "key: Host"},
		{"toml", "[[options]]"},
		{"table", "--Host"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			resp, body := serveUsage(t, http.MethodGet, "/config/usage?format="+tc.format)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d: %s", resp.StatusCode, body)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Fatalf("content-type = %q", ct)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("body missing %q:\n%s", tc.want, body)
			}
		})
	}
}

func TestUsageHandlerUnknownFormat(t *testing.T) {
	resp, body := serveUsage(t, http.MethodGet, "/config/usage?format=xml")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "unknown help format") {
		t.Fatalf("body = %s", body)
	}
}

func TestUsageHandlerMethodNotAllowed(t *testing.T) {
	resp, _ := serveUsage(t, http.MethodPost, "/config/usage")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q", allow)
	}
}
