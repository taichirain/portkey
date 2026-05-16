package credential

import (
	"testing"
)

func TestNewSecretManager_Plain(t *testing.T) {
	sm, err := NewSecretManager(nil, SecretStoragePlain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sm.storageType != SecretStoragePlain {
		t.Errorf("expected plain storage")
	}
}

func TestNewSecretManager_Hashed(t *testing.T) {
	sm, err := NewSecretManager(nil, SecretStorageHashed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sm.storageType != SecretStorageHashed {
		t.Errorf("expected hashed storage")
	}
}

func TestNewSecretManager_Encrypted_InvalidKeySize(t *testing.T) {
	_, err := NewSecretManager([]byte("short"), SecretStorageEncrypted)
	if err != ErrInvalidKeySize {
		t.Errorf("expected ErrInvalidKeySize, got %v", err)
	}
}

func TestNewSecretManager_Encrypted_ValidKeySizes(t *testing.T) {
	for _, size := range []int{16, 24, 32} {
		key := make([]byte, size)
		_, err := NewSecretManager(key, SecretStorageEncrypted)
		if err != nil {
			t.Errorf("key size %d: unexpected error: %v", size, err)
		}
	}
}

func TestPlain_StoreVerifyRetrieve(t *testing.T) {
	sm, _ := NewSecretManager(nil, SecretStoragePlain)

	stored, err := sm.Store("my-secret")
	if err != nil {
		t.Fatalf("Store error: %v", err)
	}
	if stored != "my-secret" {
		t.Errorf("expected plain passthrough, got %q", stored)
	}

	ok, err := sm.Verify(stored, "my-secret")
	if err != nil || !ok {
		t.Errorf("Verify should pass")
	}

	ok, _ = sm.Verify(stored, "wrong")
	if ok {
		t.Errorf("Verify should fail for wrong secret")
	}

	retrieved, err := sm.Retrieve(stored)
	if err != nil || retrieved != "my-secret" {
		t.Errorf("Retrieve mismatch")
	}
}

func TestHashed_StoreVerify(t *testing.T) {
	sm, _ := NewSecretManager(nil, SecretStorageHashed)

	stored, err := sm.Store("my-secret")
	if err != nil {
		t.Fatalf("Store error: %v", err)
	}
	if stored == "my-secret" {
		t.Error("hashed secret should not equal original")
	}

	ok, _ := sm.Verify(stored, "my-secret")
	if !ok {
		t.Error("Verify should pass for correct secret")
	}

	ok, _ = sm.Verify(stored, "wrong")
	if ok {
		t.Error("Verify should fail for wrong secret")
	}
}

func TestHashed_RetrieveFails(t *testing.T) {
	sm, _ := NewSecretManager(nil, SecretStorageHashed)
	stored, _ := sm.Store("my-secret")

	_, err := sm.Retrieve(stored)
	if err == nil {
		t.Error("Retrieve should fail for hashed secrets")
	}
}

func TestHashed_DifferentInputsProduceDifferentHashes(t *testing.T) {
	sm, _ := NewSecretManager(nil, SecretStorageHashed)
	h1, _ := sm.Store("secret-a")
	h2, _ := sm.Store("secret-b")
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestEncrypted_StoreVerifyRetrieve(t *testing.T) {
	key, _ := GenerateEncryptionKey(32)
	sm, _ := NewSecretManager(key, SecretStorageEncrypted)

	stored, err := sm.Store("my-secret")
	if err != nil {
		t.Fatalf("Store error: %v", err)
	}
	if stored == "my-secret" {
		t.Error("encrypted secret should not equal original")
	}

	ok, _ := sm.Verify(stored, "my-secret")
	if !ok {
		t.Error("Verify should pass for correct secret")
	}

	ok, _ = sm.Verify(stored, "wrong")
	if ok {
		t.Error("Verify should fail for wrong secret")
	}

	retrieved, err := sm.Retrieve(stored)
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if retrieved != "my-secret" {
		t.Errorf("Retrieve mismatch: got %q", retrieved)
	}
}

func TestEncrypted_DifferentCiphertextsForSameInput(t *testing.T) {
	key, _ := GenerateEncryptionKey(32)
	sm, _ := NewSecretManager(key, SecretStorageEncrypted)

	s1, _ := sm.Store("same-secret")
	s2, _ := sm.Store("same-secret")
	if s1 == s2 {
		t.Error("AES-GCM should produce different ciphertexts due to random nonce")
	}
}

func TestGenerateEncryptionKey_InvalidSize(t *testing.T) {
	_, err := GenerateEncryptionKey(10)
	if err != ErrInvalidKeySize {
		t.Errorf("expected ErrInvalidKeySize, got %v", err)
	}
}

func TestGenerateEncryptionKey_ValidSizes(t *testing.T) {
	for _, size := range []int{16, 24, 32} {
		key, err := GenerateEncryptionKey(size)
		if err != nil {
			t.Errorf("size %d: unexpected error: %v", size, err)
		}
		if len(key) != size {
			t.Errorf("expected key length %d, got %d", size, len(key))
		}
	}
}
