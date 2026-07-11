package sconf

import (
	"context"
	"errors"
	"testing"
)

// fakeResolver фиксирует вызов и заполняет поле-секрет.
type fakeResolver struct {
	called bool
	err    error
}

func (f *fakeResolver) Resolve(ctx context.Context, target any) error {
	f.called = true
	if f.err != nil {
		return f.err
	}
	if c, ok := target.(*resolveTestConfig); ok {
		c.Secret = "filled"
	}
	return nil
}

type resolveTestConfig struct {
	Host   string `default:"localhost"`
	Secret string
}

func withResolver(t *testing.T, r SecretResolver) {
	t.Helper()
	prev := secretResolver
	secretResolver = r
	t.Cleanup(func() { secretResolver = prev })
}

func TestLoadCallsResolver(t *testing.T) {
	fr := &fakeResolver{}
	withResolver(t, fr)

	cfg, err := Load[resolveTestConfig](New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fr.called {
		t.Fatal("resolver was not called")
	}
	if cfg.Secret != "filled" {
		t.Fatalf("secret = %q, resolver did not fill it", cfg.Secret)
	}
	if cfg.Host != "localhost" {
		t.Fatalf("host default lost: %q", cfg.Host)
	}
}

func TestLoadResolverErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	withResolver(t, &fakeResolver{err: sentinel})

	_, err := Load[resolveTestConfig](New(), nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestLoadNoResolverNoError(t *testing.T) {
	withResolver(t, nil) // резолвер не зарегистрирован

	cfg, err := Load[resolveTestConfig](New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secret != "" {
		t.Fatalf("secret unexpectedly set: %q", cfg.Secret)
	}
}
