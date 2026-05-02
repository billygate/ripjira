package config

import (
	"errors"
	"os"
	"sync"

	"github.com/zalando/go-keyring"
)

// KeyringService is the service name used for all keyring entries.
const KeyringService = "ripjira"

// EnvTokenVar is the environment variable consulted by EnvFallbackStore when
// the underlying keyring is unavailable.
const EnvTokenVar = "RIPJIRA_TOKEN"

// ErrSecretNotFound is returned when a secret cannot be found in the store.
var ErrSecretNotFound = errors.New("secret not found")

// SecretStore abstracts secret persistence so callers can swap implementations
// (real keychain, in-memory fake, env-var fallback) without code changes.
type SecretStore interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

// KeyringStore persists secrets via github.com/zalando/go-keyring under the
// ripjira service.
type KeyringStore struct{}

// NewKeyringStore returns a SecretStore backed by the OS keyring.
func NewKeyringStore() *KeyringStore { return &KeyringStore{} }

// Get retrieves the secret for the given account.
func (KeyringStore) Get(account string) (string, error) {
	v, err := keyring.Get(KeyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrSecretNotFound
	}
	return v, err
}

// Set stores the secret for the given account.
func (KeyringStore) Set(account, secret string) error {
	return keyring.Set(KeyringService, account, secret)
}

// Delete removes the secret for the given account.
func (KeyringStore) Delete(account string) error {
	err := keyring.Delete(KeyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrSecretNotFound
	}
	return err
}

// FakeStore is an in-memory SecretStore safe for concurrent use, used in
// tests and as a stand-in when the platform keyring is unavailable.
type FakeStore struct {
	mu      sync.Mutex
	secrets map[string]string
}

// NewFakeStore returns an empty in-memory SecretStore.
func NewFakeStore() *FakeStore {
	return &FakeStore{secrets: map[string]string{}}
}

// Get returns the stored secret or ErrSecretNotFound.
func (f *FakeStore) Get(account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.secrets[account]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

// Set stores a secret in memory.
func (f *FakeStore) Set(account, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[account] = secret
	return nil
}

// Delete removes a secret; missing keys return ErrSecretNotFound.
func (f *FakeStore) Delete(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.secrets[account]; !ok {
		return ErrSecretNotFound
	}
	delete(f.secrets, account)
	return nil
}

// EnvFallbackStore wraps an inner SecretStore and consults the RIPJIRA_TOKEN
// environment variable when the inner Get fails (e.g. keyring unavailable on
// the host). Set and Delete always delegate to the inner store — env-var
// secrets are read-only.
type EnvFallbackStore struct {
	Inner SecretStore
	// Getenv is overridable for tests; defaults to os.Getenv.
	Getenv func(string) string
}

// NewEnvFallbackStore returns a store that reads RIPJIRA_TOKEN as a fallback
// for any account when inner.Get fails.
func NewEnvFallbackStore(inner SecretStore) *EnvFallbackStore {
	return &EnvFallbackStore{Inner: inner, Getenv: os.Getenv}
}

// Get tries the inner store first; on error, falls back to RIPJIRA_TOKEN.
func (e *EnvFallbackStore) Get(account string) (string, error) {
	if e.Inner != nil {
		v, err := e.Inner.Get(account)
		if err == nil {
			return v, nil
		}
	}
	getenv := e.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if v := getenv(EnvTokenVar); v != "" {
		return v, nil
	}
	return "", ErrSecretNotFound
}

// Set delegates to the inner store. With no inner store, returns an error.
func (e *EnvFallbackStore) Set(account, secret string) error {
	if e.Inner == nil {
		return errors.New("env fallback store is read-only")
	}
	return e.Inner.Set(account, secret)
}

// Delete delegates to the inner store. With no inner store, returns an error.
func (e *EnvFallbackStore) Delete(account string) error {
	if e.Inner == nil {
		return errors.New("env fallback store is read-only")
	}
	return e.Inner.Delete(account)
}
