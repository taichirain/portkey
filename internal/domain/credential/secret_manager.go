package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var (
	ErrSecretTooShort     = errors.New("secret too short")
	ErrInvalidKeySize     = errors.New("invalid key size for AES")
	ErrEncryptionFailed   = errors.New("encryption failed")
	ErrDecryptionFailed   = errors.New("decryption failed")
)

type SecretStorageType string

const (
	SecretStoragePlain    SecretStorageType = "plain"
	SecretStorageHashed   SecretStorageType = "hashed"
	SecretStorageEncrypted SecretStorageType = "encrypted"
)

type SecretManager struct {
	encryptionKey []byte
	storageType   SecretStorageType
}

func NewSecretManager(encryptionKey []byte, storageType SecretStorageType) (*SecretManager, error) {
	if storageType == SecretStorageEncrypted {
		keyLen := len(encryptionKey)
		if keyLen != 16 && keyLen != 24 && keyLen != 32 {
			return nil, ErrInvalidKeySize
		}
	}

	return &SecretManager{
		encryptionKey: encryptionKey,
		storageType:   storageType,
	}, nil
}

func (m *SecretManager) Store(secret string) (string, error) {
	switch m.storageType {
	case SecretStorageHashed:
		return m.hashSecret(secret), nil
	case SecretStorageEncrypted:
		return m.encryptSecret(secret)
	default:
		return secret, nil
	}
}

func (m *SecretManager) Verify(storedSecret, inputSecret string) (bool, error) {
	switch m.storageType {
	case SecretStorageHashed:
		inputHash := m.hashSecret(inputSecret)
		return storedSecret == inputHash, nil
	case SecretStorageEncrypted:
		decrypted, err := m.decryptSecret(storedSecret)
		if err != nil {
			return false, err
		}
		return decrypted == inputSecret, nil
	default:
		return storedSecret == inputSecret, nil
	}
}

func (m *SecretManager) Retrieve(storedSecret string) (string, error) {
	switch m.storageType {
	case SecretStorageHashed:
		return "", errors.New("cannot retrieve hashed secret")
	case SecretStorageEncrypted:
		return m.decryptSecret(storedSecret)
	default:
		return storedSecret, nil
	}
}

func (m *SecretManager) hashSecret(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return base64.StdEncoding.EncodeToString(hash[:])
}

func (m *SecretManager) encryptSecret(secret string) (string, error) {
	block, err := aes.NewCipher(m.encryptionKey)
	if err != nil {
		return "", ErrEncryptionFailed
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ErrEncryptionFailed
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", ErrEncryptionFailed
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (m *SecretManager) decryptSecret(encryptedSecret string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedSecret)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	block, err := aes.NewCipher(m.encryptionKey)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrDecryptionFailed
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	return string(plaintext), nil
}

func GenerateEncryptionKey(keySize int) ([]byte, error) {
	if keySize != 16 && keySize != 24 && keySize != 32 {
		return nil, ErrInvalidKeySize
	}

	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}

	return key, nil
}
