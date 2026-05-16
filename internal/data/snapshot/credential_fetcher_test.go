package snapshot

import (
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/credential"
)

func TestSnapshotCredentialFetcher_GetByKey_Found(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeKeyAuth, Key: "my-api-key", Secret: "s", Enabled: true,
	}
	snap.AddCredential(cred)

	fetcher := NewSnapshotCredentialFetcher(snap)
	got, err := fetcher.GetByKey("my-api-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Key != "my-api-key" {
		t.Error("expected to find credential")
	}
}

func TestSnapshotCredentialFetcher_GetByKey_NotFound(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	fetcher := NewSnapshotCredentialFetcher(snap)

	got, err := fetcher.GetByKey("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing key")
	}
}

func TestSnapshotCredentialFetcher_MultipleCredentials(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	cid := uuid.New()
	snap.AddCredential(&credential.Credential{
		ID: uuid.New(), ConsumerID: cid,
		Type: credential.TypeKeyAuth, Key: "key-1", Enabled: true,
	})
	snap.AddCredential(&credential.Credential{
		ID: uuid.New(), ConsumerID: cid,
		Type: credential.TypeJWTAuth, Key: "jwt-iss", Enabled: true,
	})

	fetcher := NewSnapshotCredentialFetcher(snap)

	c1, _ := fetcher.GetByKey("key-1")
	if c1 == nil || c1.Type != credential.TypeKeyAuth {
		t.Error("expected key-auth credential")
	}

	c2, _ := fetcher.GetByKey("jwt-iss")
	if c2 == nil || c2.Type != credential.TypeJWTAuth {
		t.Error("expected jwt-auth credential")
	}
}
