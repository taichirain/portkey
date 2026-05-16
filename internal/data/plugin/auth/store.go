package auth

import (
	"sync"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/credential"
)

type CredentialStore interface {
	GetByKey(key string) (*credential.Credential, error)
	GetByConsumerID(consumerID uuid.UUID) ([]*credential.Credential, error)
}

type InMemoryCredentialStore struct {
	mu                  sync.RWMutex
	credentialsByKey    map[string]*credential.Credential
	credentialsByConsumer map[uuid.UUID][]*credential.Credential
}

func NewInMemoryCredentialStore() *InMemoryCredentialStore {
	return &InMemoryCredentialStore{
		credentialsByKey:    make(map[string]*credential.Credential),
		credentialsByConsumer: make(map[uuid.UUID][]*credential.Credential),
	}
}

func (s *InMemoryCredentialStore) Add(cred *credential.Credential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentialsByKey[cred.Key] = cred
	s.credentialsByConsumer[cred.ConsumerID] = append(s.credentialsByConsumer[cred.ConsumerID], cred)
}

func (s *InMemoryCredentialStore) GetByKey(key string) (*credential.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cred, ok := s.credentialsByKey[key]
	if !ok {
		return nil, nil
	}
	return cred, nil
}

func (s *InMemoryCredentialStore) GetByConsumerID(consumerID uuid.UUID) ([]*credential.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	creds, ok := s.credentialsByConsumer[consumerID]
	if !ok {
		return []*credential.Credential{}, nil
	}
	result := make([]*credential.Credential, len(creds))
	copy(result, creds)
	return result, nil
}

func (s *InMemoryCredentialStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentialsByKey = make(map[string]*credential.Credential)
	s.credentialsByConsumer = make(map[uuid.UUID][]*credential.Credential)
}
