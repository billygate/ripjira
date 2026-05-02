package config

import (
	"errors"
	"testing"
)

func TestFakeStore_RoundTrip(t *testing.T) {
	s := NewFakeStore()

	if _, err := s.Get("alice@example.com"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get on empty store: want ErrSecretNotFound, got %v", err)
	}

	if err := s.Set("alice@example.com", "tok-1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("alice@example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "tok-1" {
		t.Fatalf("Get: want tok-1, got %q", got)
	}

	if err := s.Set("alice@example.com", "tok-2"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got, _ = s.Get("alice@example.com")
	if got != "tok-2" {
		t.Fatalf("after overwrite: want tok-2, got %q", got)
	}

	if err := s.Delete("alice@example.com"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete("alice@example.com"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Delete missing: want ErrSecretNotFound, got %v", err)
	}
	if _, err := s.Get("alice@example.com"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after delete: want ErrSecretNotFound, got %v", err)
	}
}

func TestFakeStore_Isolation(t *testing.T) {
	s := NewFakeStore()
	_ = s.Set("a@example.com", "secret-a")
	_ = s.Set("b@example.com", "secret-b")

	a, _ := s.Get("a@example.com")
	b, _ := s.Get("b@example.com")
	if a != "secret-a" || b != "secret-b" {
		t.Fatalf("accounts collided: a=%q b=%q", a, b)
	}
	if err := s.Delete("a@example.com"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}
	if _, err := s.Get("b@example.com"); err != nil {
		t.Fatalf("Delete a affected b: %v", err)
	}
}

func TestEnvFallbackStore_InnerHit(t *testing.T) {
	inner := NewFakeStore()
	_ = inner.Set("alice@example.com", "from-keyring")

	s := &EnvFallbackStore{
		Inner:  inner,
		Getenv: func(string) string { return "from-env" },
	}
	got, err := s.Get("alice@example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "from-keyring" {
		t.Fatalf("expected inner to win: got %q", got)
	}
}

func TestEnvFallbackStore_FallsBackToEnv(t *testing.T) {
	inner := NewFakeStore() // empty
	s := &EnvFallbackStore{
		Inner: inner,
		Getenv: func(k string) string {
			if k == EnvTokenVar {
				return "env-token"
			}
			return ""
		},
	}
	got, err := s.Get("any-account")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "env-token" {
		t.Fatalf("want env-token, got %q", got)
	}
}

func TestEnvFallbackStore_NoInnerNoEnv(t *testing.T) {
	s := &EnvFallbackStore{
		Inner:  NewFakeStore(),
		Getenv: func(string) string { return "" },
	}
	if _, err := s.Get("acc"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("want ErrSecretNotFound, got %v", err)
	}
}

func TestEnvFallbackStore_SetDeleteDelegate(t *testing.T) {
	inner := NewFakeStore()
	s := NewEnvFallbackStore(inner)

	if err := s.Set("alice", "tok"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := inner.Get("alice")
	if err != nil || got != "tok" {
		t.Fatalf("inner.Get after Set: got=%q err=%v", got, err)
	}
	if err := s.Delete("alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := inner.Get("alice"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("inner.Get after Delete: want ErrSecretNotFound, got %v", err)
	}
}

func TestEnvFallbackStore_NilInnerReadOnly(t *testing.T) {
	s := &EnvFallbackStore{
		Inner: nil,
		Getenv: func(k string) string {
			if k == EnvTokenVar {
				return "env-only"
			}
			return ""
		},
	}
	got, err := s.Get("acc")
	if err != nil || got != "env-only" {
		t.Fatalf("Get: got=%q err=%v", got, err)
	}
	if err := s.Set("acc", "x"); err == nil {
		t.Fatalf("Set on read-only store: want error, got nil")
	}
	if err := s.Delete("acc"); err == nil {
		t.Fatalf("Delete on read-only store: want error, got nil")
	}
}

// Smoke test that KeyringStore satisfies SecretStore at compile time without
// actually calling the OS keychain (calls are exercised in keyring_real_test.go
// behind the `keyring` build tag).
func TestKeyringStore_ImplementsInterface(_ *testing.T) {
	var _ SecretStore = NewKeyringStore()
}
