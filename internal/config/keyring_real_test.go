//go:build keyring

package config

import (
	"errors"
	"testing"
)

// TestKeyringStore_RoundTrip exercises the real OS keyring. It is gated behind
// the `keyring` build tag so CI and developer machines without an unlocked
// keychain stay green by default. Run with: go test -tags keyring ./...
func TestKeyringStore_RoundTrip(t *testing.T) {
	const account = "ripjira-test@example.invalid"
	s := NewKeyringStore()

	// Best-effort cleanup before and after to avoid leaving entries behind.
	_ = s.Delete(account)
	t.Cleanup(func() { _ = s.Delete(account) })

	if err := s.Set(account, "tok"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(account)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "tok" {
		t.Fatalf("Get: want tok, got %q", got)
	}
	if err := s.Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(account); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete: want ErrSecretNotFound, got %v", err)
	}
}
