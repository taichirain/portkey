package auth

import (
	"testing"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/domain/credential"
)

func TestInMemoryCredentialStore_AddAndGetByKey(t *testing.T) {
	store := NewInMemoryCredentialStore()
	cid := uuid.New()
	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: cid,
		Type: credential.TypeKeyAuth, Key: "my-key", Enabled: true,
	}
	store.Add(cred)

	got, err := store.GetByKey("my-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Key != "my-key" {
		t.Errorf("expected to find credential by key")
	}
}

func TestInMemoryCredentialStore_GetByKey_NotFound(t *testing.T) {
	store := NewInMemoryCredentialStore()
	got, err := store.GetByKey("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing key")
	}
}

func TestInMemoryCredentialStore_GetByConsumerID(t *testing.T) {
	store := NewInMemoryCredentialStore()
	cid := uuid.New()
	store.Add(&credential.Credential{
		ID: uuid.New(), ConsumerID: cid,
		Type: credential.TypeKeyAuth, Key: "k1", Enabled: true,
	})
	store.Add(&credential.Credential{
		ID: uuid.New(), ConsumerID: cid,
		Type: credential.TypeJWTAuth, Key: "k2", Enabled: true,
	})

	creds, err := store.GetByConsumerID(cid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(creds) != 2 {
		t.Errorf("expected 2 credentials, got %d", len(creds))
	}
}

func TestInMemoryCredentialStore_GetByConsumerID_Empty(t *testing.T) {
	store := NewInMemoryCredentialStore()
	creds, err := store.GetByConsumerID(uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("expected 0 credentials, got %d", len(creds))
	}
}

func TestInMemoryCredentialStore_Clear(t *testing.T) {
	store := NewInMemoryCredentialStore()
	store.Add(&credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeKeyAuth, Key: "k", Enabled: true,
	})
	store.Clear()

	got, _ := store.GetByKey("k")
	if got != nil {
		t.Error("expected empty store after Clear")
	}
}

func TestRegisterAuthPlugins(t *testing.T) {
	reg := pluginPkg.NewPluginRegistry()
	RegisterAuthPlugins(reg)

	if _, ok := reg.Get("key-auth"); !ok {
		t.Error("expected key-auth to be registered")
	}
	if _, ok := reg.Get("jwt-auth"); !ok {
		t.Error("expected jwt-auth to be registered")
	}
}
